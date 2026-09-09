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
	"syscall"
	"time"

	"treasury/api"
	"treasury/internal/buildid"
	"treasury/internal/httpapi"
	"treasury/verify"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":"+envOr("PORT", "8080")), "address to listen on")
	maxImage := flag.Int64("max-image", 10<<20, "largest image accepted, in bytes")
	maxBatch := flag.Int64("max-batch", 100<<20, "largest batch accepted, in bytes")
	timeout := flag.Duration("timeout", 60*time.Second, "how long one verification may take")
	cores := flag.Int("cores", 0, "cores one verification may use; 0 is every core")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	id := buildid.Get()
	log.Info("starting", "commit", id.Commit, "modified", id.Modified, "go", id.Go)

	eng, err := verify.New(verify.Options{Cores: float64(*cores)})
	if err != nil {
		// A service that cannot read is not a service. It says so and
		// stops, rather than starting and answering every request with
		// an apology.
		log.Error("the reader could not be loaded", "error", err)
		os.Exit(1)
	}
	defer eng.Close()

	srv := &httpapi.Server{
		Engine: eng, Doc: api.Spec,
		MaxImage: *maxImage, MaxBatch: *maxBatch, Timeout: *timeout,
	}
	h := httpapi.Log(log, srv.Handler())

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
