package httpapi

import (
	"io"
	"log/slog"
	"net/http"
)

// Service is the whole service: the routes, and the middleware around
// them, in the order they have to be in.
//
// It exists because there used to be two of these — one written out in
// cmd/serve and one implied by every test calling Server.Handler directly
// — and the tests were therefore blind to everything the chain does. That
// is how a wrapper that swallowed Flush went unnoticed while the README,
// the specification and the doc all said the batch streamed. There is one
// chain now, and a test that does not use it is testing something the
// service is not.
//
// The order is outermost first, and each layer is where it is for a
// reason. Recover is outermost so a panic anywhere inside becomes a 500
// rather than a dead process. Log is next so it sees the status Recover
// produced. Secure sets headers on everything, including refusals. Then
// the two that cost the caller something: the day's budget, and the
// per-caller bucket. Both refuse before the engine runs, which is the
// point of them.
func Service(srv *Server, log *slog.Logger, limits Limits, budget *Budget) http.Handler {
	var h http.Handler = srv.Handler()
	h = NewThrottle(limits.Rate, limits.Burst).Middleware(h)
	if budget != nil {
		h = budget.Middleware(h)
	}
	h = Secure(h)
	if log == nil {
		// A caller without a logger still gets one, because Recover
		// writes to it from inside a deferred recover, where a nil
		// pointer would turn a panic into a second panic.
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	h = Log(log, h)
	h = Recover(log, h)
	return h
}
