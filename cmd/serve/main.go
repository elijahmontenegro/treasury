// Command serve runs the verification engine as an HTTP service.
//
//	serve -addr :8080
//
// One binary, no database, nothing on disk. The engine's models are
// embedded, so the container needs the ONNX Runtime library and nothing
// else, and an uploaded image lives only as long as the request that
// brought it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"treasury/api"
	"treasury/internal/buildid"
	"treasury/internal/cpu"
	"treasury/internal/httpapi"
	"treasury/verify"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":"+envOr("PORT", "8080")), "address to listen on")
	maxImage := flag.Int64("max-image", 10<<20, "largest image accepted, in bytes")
	maxBatch := flag.Int64("max-batch", 100<<20, "largest batch accepted, in bytes")
	timeout := flag.Duration("timeout", 60*time.Second, "how long one verification may take")
	cores := flag.Int("cores", 0, "cores one verification may use; 0 is this process's own share")
	rate := flag.Float64("rate", 0, "requests a second per caller; 0 keeps the default")
	burst := flag.Float64("burst", 10, "requests a caller may make at once")
	daily := flag.Float64("daily-seconds", 0, "inference seconds a day, all callers; 0 keeps the default")
	flag.Parse()

	// The Go scheduler sizes itself from the affinity mask, which inside a
	// container is the node's cores rather than this container's quota, so
	// it is told the quota. The engine's pool reads the same number.
	have := cpu.Available()
	runtime.GOMAXPROCS(have)

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	id := buildid.Get()
	log.Info("starting", "commit", id.Commit, "modified", id.Modified, "go", id.Go,
		"cores", have, "cores_reported", runtime.NumCPU())

	eng, err := verify.New(verify.Options{Cores: float64(*cores)})
	if err != nil {
		// A service that cannot read is not a service. It says so and
		// stops, rather than starting and answering every request with
		// an apology.
		log.Error("the reader could not be loaded", "error", err)
		os.Exit(1)
	}
	defer eng.Close()

	limits := httpapi.DefaultLimits()
	if *rate > 0 {
		limits.Rate, limits.Burst = *rate, *burst
	}
	if *daily > 0 {
		limits.DailySeconds = *daily
	}
	budget := httpapi.NewBudget(limits.DailySeconds)

	srv := &httpapi.Server{
		Engine: eng, Doc: api.Spec,
		MaxImage: *maxImage, MaxBatch: *maxBatch, Timeout: *timeout,
		Limits: limits, Budget: budget,
	}

	// Outermost first. Everything here refuses before the engine runs,
	// which is the point: a verification costs a second of every core,
	// and anything refusable for the price of a header must be refused
	// there.
	var h http.Handler = srv.Handler()
	h = httpapi.NewThrottle(limits.Rate, limits.Burst).Middleware(h)
	h = budget.Middleware(h)
	h = httpapi.Secure(h)
	h = httpapi.Log(log, h)
	h = httpapi.Recover(log, h)

	s := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		// A verification may take seconds and the whole point of the
		// service is that it says so; the write timeout allows for the
		// slowest label plus the response.
		WriteTimeout: *timeout + 30*time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Stop on a signal rather than being killed, so a request in flight
	// finishes and its image is discarded the way every other one is.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.Shutdown(shut)
	}()

	log.Info("listening", "addr", *addr)
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("stopped", "error", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "stopped")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
