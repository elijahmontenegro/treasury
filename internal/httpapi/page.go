package httpapi

import (
	_ "embed"
	"net/http"
)

// page is the operator's screen, embedded so the binary carries it and
// works with no network and no external assets. There is no framework and
// no CDN: a reviewer's browser should not have to reach anywhere to show
// the answer, and a service that runs in a locked-down environment should
// not stop working because a stylesheet lives somewhere else.
//
//go:embed page.html
var page []byte

// Page serves it.
func (s *Server) Page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}
