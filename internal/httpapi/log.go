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

// recorder counts what went out so the log line can say.
//
// It embeds the ResponseWriter as an interface, which promotes exactly
// three methods — Header, Write, WriteHeader — and hides every other
// thing the real writer can do. That is not a detail. The batch handler
// asks `w.(http.Flusher)` whether it can push a line out as it is
// written, and through this wrapper the answer was no, so the NDJSON
// stream the README and the specification both promise did not stream:
// six labels arrived in one piece after all six had been verified. No
// test saw it, because every test called Handler directly and never built
// the chain this sits in.
//
// So the two things a wrapper owes the writer underneath it are here.
// Flush passes through, and Unwrap lets http.ResponseController find the
// real writer for the deadline the batch stream needs.
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

// Flush is the one that mattered. A wrapper that swallows it turns a
// stream into a buffer, silently.
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap is how http.ResponseController reaches past a wrapper. Without
// it, SetWriteDeadline fails with ErrNotSupported and a long batch is cut
// off by the server's write timeout instead of being given the time the
// caller was told it could have.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
