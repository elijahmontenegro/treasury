package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// Log records what was asked and what was answered, and nothing about
// what was in the image. A label under review is somebody's commercial
// business and the service is not a place it accumulates.
func Log(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("request",
			"method", r.Method, "path", r.URL.Path,
			"status", rec.code, "bytes", rec.n,
			"ms", time.Since(start).Milliseconds())
	})
}

type recorder struct {
	http.ResponseWriter
	code int
	n    int
}

func (r *recorder) WriteHeader(c int) {
	r.code = c
	r.ResponseWriter.WriteHeader(c)
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.n += n
	return n, err
}
