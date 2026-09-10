package httpapi_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"treasury/api"
	"treasury/internal/httpapi"
	"treasury/verify"
)

// countingEngine is not an engine at all: it is a counter that fails the
// test if it is ever reached. Every refusal in this file must happen
// before the engine runs, and the only way to prove that is to have
// nothing behind the middleware that could run.
type reached struct{ n atomic.Int64 }

func (c *reached) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.n.Add(1)
		w.WriteHeader(http.StatusOK)
	})
}

// bomb builds a PNG that declares itself enormous and is a few hundred
// bytes on the wire: 60,000 by 60,000 pixels, which is 14 GB decoded and
// which no body limit can catch, because the body is tiny.
func bomb(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	chunk := func(kind string, data []byte) {
		binary.Write(&b, binary.BigEndian, uint32(len(data)))
		payload := append([]byte(kind), data...)
		b.Write(payload)
		binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(payload))
	}
	var ihdr bytes.Buffer
	binary.Write(&ihdr, binary.BigEndian, uint32(60000)) // width
	binary.Write(&ihdr, binary.BigEndian, uint32(60000)) // height
	ihdr.Write([]byte{8, 0, 0, 0, 0})                    // 8-bit greyscale
	chunk("IHDR", ihdr.Bytes())
	var idat bytes.Buffer
	zw := zlib.NewWriter(&idat)
	zw.Write(make([]byte, 1024))
	zw.Close()
	chunk("IDAT", idat.Bytes())
	chunk("IEND", nil)
	return b.Bytes()
}

func multipartBody(t *testing.T, field, name string, content []byte, extra map[string]string) (string, *bytes.Buffer) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	f, err := mw.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(content)
	for k, v := range extra {
		mw.WriteField(k, v)
	}
	mw.Close()
	return mw.FormDataContentType(), &body
}

// TestNothingReachesTheEngineThatShouldNot is step 30e's gate: an
// oversized body, a non-image, a decompression bomb and a burst past the
// limit, each refused before the engine runs.
//
// "Before the engine runs" is not asserted, it is arranged: what sits
// behind the middleware in this test is a counter, and any refusal that
// let a request through would show up as a count.
func TestNothingReachesTheEngineThatShouldNot(t *testing.T) {
	claims := `{"beverage":"wine","brand":"X"}`

	// A real engine, and 413 exactly. This subtest used to build a Server
	// with no engine and accept "413 or 503", so the handler's own
	// not-ready answer satisfied it and the whole thing passed with the
	// body limit deleted - checked, it did. The two subtests below it had
	// the same fault and were fixed at step 30e; this one was left, which
	// is why it is worth saying that a guard against vacuity has to be
	// applied to every case rather than to the ones that come to mind.
	t.Run("a body larger than the limit", func(t *testing.T) {
		eng, err := verify.New(verify.Options{})
		if err != nil {
			needReader(t, err)
		}
		defer eng.Close()
		srv := &httpapi.Server{Engine: eng, MaxImage: 1 << 20, Limits: httpapi.DefaultLimits()}
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		ct, body := multipartBody(t, "image", "big.png", make([]byte, 3<<20),
			map[string]string{"claims": claims})
		resp, err := http.Post(ts.URL+"/verify", ct, body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d for a 3 MB body against a 1 MB limit, want 413", resp.StatusCode)
		}
		var e api.Error
		json.NewDecoder(resp.Body).Decode(&e)
		// Which of the two size checks answered matters, and only one of
		// them is worth having. The body limit refuses while reading and
		// stops; the header-size check that follows it refuses after the
		// whole upload has been taken in, which for a body of any size is
		// the cost this layer exists to avoid. They word their refusals
		// differently, so the message says which fired - and with the
		// body limit deleted this test now fails instead of being caught
		// by the second check and passing anyway, which it did.
		if !strings.Contains(e.Error, "larger than this service accepts") {
			t.Errorf("the body was taken in whole and refused afterwards, "+
				"rather than refused while being read: %q", e.Error)
		}
	})

	// The bomb and the non-image need a REAL engine behind them. With no
	// engine the handler answers 503 before it ever looks at the image,
	// so the test would pass whether or not the pixel check existed -
	// which is the vacuous guard step 30a spent a paragraph on. With an
	// engine loaded, a 413 naming the dimensions can only have come from
	// the check reading the header.
	t.Run("a decompression bomb", func(t *testing.T) {
		eng, err := verify.New(verify.Options{})
		if err != nil {
			needReader(t, err)
		}
		defer eng.Close()
		srv := &httpapi.Server{Engine: eng, MaxImage: 10 << 20, Limits: httpapi.DefaultLimits()}
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		ct, body := multipartBody(t, "image", "bomb.png", bomb(t),
			map[string]string{"claims": claims})
		resp, err := http.Post(ts.URL+"/verify", ct, body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d for a 60,000 by 60,000 image, want 413", resp.StatusCode)
		}
		var e api.Error
		json.NewDecoder(resp.Body).Decode(&e)
		if !strings.Contains(e.Error, "60000 by 60000") {
			t.Errorf("the refusal does not name the dimensions it read from the header: %q", e.Error)
		}
	})

	t.Run("a burst past the limit", func(t *testing.T) {
		var c reached
		// One request a second with two in hand: the third in a burst is
		// refused, and the counter proves it never got through.
		h := httpapi.NewThrottle(1, 2).Middleware(c.handler())
		ts := httptest.NewServer(h)
		defer ts.Close()
		allowed, refused := 0, 0
		for range 8 {
			resp, err := http.Post(ts.URL+"/verify", "text/plain", strings.NewReader("x"))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				refused++
			} else {
				allowed++
			}
		}
		if refused == 0 {
			t.Error("a burst of eight was not throttled at all")
		}
		if got := c.n.Load(); got != int64(allowed) {
			t.Errorf("%d requests reached past the throttle, %d were allowed", got, allowed)
		}
	})

	t.Run("the day's budget spent", func(t *testing.T) {
		var c reached
		b := httpapi.NewBudget(1)
		b.Spend(2 * 1e9) // two seconds, past a one-second budget
		ts := httptest.NewServer(b.Middleware(c.handler()))
		defer ts.Close()
		resp, err := http.Post(ts.URL+"/verify", "text/plain", strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status %d past the day's budget, want 503", resp.StatusCode)
		}
		if c.n.Load() != 0 {
			t.Error("a request reached the engine past the day's budget")
		}
		var e api.Error
		json.NewDecoder(resp.Body).Decode(&e)
		if !strings.Contains(e.Error, "today") {
			t.Errorf("the refusal does not say why: %q", e.Error)
		}
	})
}

// TestANonImageIsRefused keeps the plainest case honest: something that
// is not an image at all is refused with a sentence, not a stack trace.
func TestANonImageIsRefused(t *testing.T) {
	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()
	srv := &httpapi.Server{Engine: eng, MaxImage: 10 << 20, Limits: httpapi.DefaultLimits()}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	// A claim as well as a beverage, so what is under test is the file
	// and not the application: an application that asks nothing is
	// refused before the image is looked at, which is cheaper and is the
	// right order, but it would answer this request instead.
	ct, body := multipartBody(t, "image", "notes.txt", []byte("this is not an image"),
		map[string]string{"claims": `{"beverage":"wine","brand":"X"}`})
	resp, err := http.Post(ts.URL+"/verify", ct, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d for a text file, want 400", resp.StatusCode)
	}
	var e api.Error
	json.NewDecoder(resp.Body).Decode(&e)
	if !strings.Contains(e.Error, "PNG") {
		t.Errorf("the refusal does not say what to send: %q", e.Error)
	}
}

// TestTheHeadersAreSet checks what a browser is told, since this service
// serves a page as well as an API.
func TestTheHeadersAreSet(t *testing.T) {
	srv := &httpapi.Server{}
	ts := httptest.NewServer(httpapi.Secure(srv.Handler()))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s is %q, want %q", k, got, want)
		}
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the policy does not carry %q: %q", want, csp)
		}
	}
}

// TestAPanicBecomesAnError keeps one bad request from taking the process
// down with every other request in flight.
func TestAPanicBecomesAnError(t *testing.T) {
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	ts := httptest.NewServer(httpapi.Recover(quietLogger(), boom))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d after a panic, want 500", resp.StatusCode)
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
