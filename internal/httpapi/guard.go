package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// This file is what stands between the engine and a public address.
//
// Everything here refuses BEFORE the engine runs, and that ordering is
// the whole point rather than a detail. A verification costs a second or
// two of every core the machine has; anything that can be refused for the
// price of reading a header must be, or a single caller sending nonsense
// as fast as it can is a denial of service against every other caller.
//
// It is all in-process. There is no Redis, no sidecar and no external
// rate limiter, because the service is one binary and adding a dependency
// to defend it would be a larger change to what this is than the defence
// is worth. The cost is that the limits are per instance rather than per
// deployment, which is stated in the README rather than glossed.

// Limits is what the service will accept.
type Limits struct {
	// MaxPixels is the largest image the engine will be given, read from
	// the image's own header before a single pixel is decoded. A 200 kB
	// PNG can declare 60,000 by 60,000 and cost 14 GB to decode - the
	// decompression bomb - and no body limit catches it, because the body
	// is small. This does.
	MaxPixels int

	// PerIP is how many requests one caller may make, as a token bucket:
	// Burst at once, refilled at Rate a second.
	Rate  float64
	Burst float64

	// DailySeconds is how much inference the service will do in a day,
	// across all callers. Past it every verification answers 503 until
	// the day turns. It is the backstop the per-IP bucket is not: a
	// hundred callers each inside their own limit can still cost more
	// machine time than anyone intends to pay for.
	DailySeconds float64
}

// DefaultLimits are the ones the service runs with.
func DefaultLimits() Limits {
	return Limits{
		// 50 megapixels: larger than any label in the fifty (the biggest
		// is 2561 by 5391, under 14) and far below what a bomb declares.
		MaxPixels: 50_000_000,
		// Two a second with ten in hand: a reviewer working through a
		// pile never notices, and a script cannot outrun the engine.
		Rate: 2, Burst: 10,
		// Eight core-hours a day.
		DailySeconds: 8 * 3600,
	}
}

// Recover turns a panic into a 500 and a log line, so one request that
// finds a bug does not take the process down with every other request in
// flight.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic", "path", r.URL.Path, "value", v,
					"stack", string(debug.Stack()))
				// The response may be part written, in which case this
				// header is ignored and the caller sees a truncated
				// body, which is the honest outcome.
				fail(w, http.StatusInternalServerError,
					"Something went wrong reading that. Please try again.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Secure sets the headers a browser needs to be told, since this service
// serves a page as well as an API.
func Secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// The page carries its own styles and scripts inline and fetches
		// nothing, so the policy can be as tight as the page allows:
		// itself, and images it made itself.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; img-src 'self' data:; "+
				"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// bucket is one caller's allowance.
type bucket struct {
	tokens float64
	seen   time.Time
}

// Throttle is a token bucket per caller, keyed on the address the proxy
// says the request came from.
//
// X-Forwarded-For is taken from the LAST hop rather than the first,
// because a caller can write whatever it likes into that header and only
// the value the trusted proxy appended is worth anything. Behind Cloud
// Run that is the client address; behind nothing at all it is the socket.
type Throttle struct {
	rate, burst float64
	mu          sync.Mutex
	seen        map[string]*bucket
	now         func() time.Time
}

func NewThrottle(rate, burst float64) *Throttle {
	return &Throttle{rate: rate, burst: burst, seen: map[string]*bucket{}, now: time.Now}
}

// Allow says whether this caller may make a request now.
func (t *Throttle) Allow(key string) bool {
	if t.rate <= 0 {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	b, ok := t.seen[key]
	if !ok {
		// A map keyed by caller is itself somewhere memory accumulates,
		// so idle callers are dropped whenever it grows.
		if len(t.seen) > 4096 {
			for k, v := range t.seen {
				if now.Sub(v.seen) > 10*time.Minute {
					delete(t.seen, k)
				}
			}
		}
		b = &bucket{tokens: t.burst, seen: now}
		t.seen[key] = b
	}
	b.tokens += now.Sub(b.seen).Seconds() * t.rate
	if b.tokens > t.burst {
		b.tokens = t.burst
	}
	b.seen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Middleware refuses a caller past its allowance, before anything is read.
func (t *Throttle) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r) // the page and the health check are cheap
			return
		}
		if !t.Allow(callerOf(r)) {
			w.Header().Set("Retry-After", "1")
			fail(w, http.StatusTooManyRequests,
				"That is more labels at once than this service accepts. Wait a moment and try again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// callerOf is who to charge for the request.
func callerOf(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// The last entry is the one the nearest trusted proxy appended;
		// everything before it the caller could have written itself.
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Budget is how much inference the service will do in a day.
type Budget struct {
	limit float64
	mu    sync.Mutex
	spent float64
	day   int
	now   func() time.Time
}

func NewBudget(seconds float64) *Budget {
	return &Budget{limit: seconds, now: time.Now}
}

// Left reports whether there is budget to do more work.
func (b *Budget) Left() bool {
	if b.limit <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	return b.spent < b.limit
}

// Spend records work done.
func (b *Budget) Spend(d time.Duration) {
	if b.limit <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	b.spent += d.Seconds()
}

func (b *Budget) rollover() {
	if day := b.now().UTC().YearDay(); day != b.day {
		b.day, b.spent = day, 0
	}
}

// Middleware refuses work once the day's budget is gone.
func (b *Budget) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && !b.Left() {
			w.Header().Set("Retry-After", "3600")
			fail(w, http.StatusServiceUnavailable,
				"This service has done as much reading as it is allowed today. Try again tomorrow.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// errTooManyPixels is what a decompression bomb gets.
var errTooManyPixels = errors.New("too many pixels")

// decodeImage reads an image's header, refuses it if the dimensions it
// declares are more than the service will decode, and only then decodes
// it.
//
// This is the check a body limit cannot make. A PNG of a few hundred
// kilobytes can declare sixty thousand pixels square, which is fourteen
// gigabytes once decoded, and the body limit sees a small body. The
// header says what it intends to cost before any of it is paid.
func decodeImage(r io.Reader, maxPixels int) (image.Image, error) {
	var head bytes.Buffer
	cfg, _, err := image.DecodeConfig(io.TeeReader(io.LimitReader(r, 1<<20), &head))
	if err != nil {
		return nil, err
	}
	if maxPixels > 0 && cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("%w: the image says it is %d by %d",
			errTooManyPixels, cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(io.MultiReader(bytes.NewReader(head.Bytes()), r))
	return img, err
}
