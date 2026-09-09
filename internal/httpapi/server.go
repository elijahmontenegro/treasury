// Package httpapi serves the verification engine over HTTP.
//
// The handlers are written by hand against the interface generated from
// `api/openapi.yaml`, so a route the specification describes and this
// package does not serve fails to compile rather than 404ing in
// production.
//
// Nothing is stored. An uploaded image is decoded, verified and dropped
// when the response is written; there is no database and no temporary
// file. What comes back is what the CLI would have printed, plus the
// evidence crops, which are cut from the image while it is still in
// memory and are the only reason it is held as long as it is.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"time"

	"treasury/api"
	"treasury/internal/buildid"
	"treasury/ttb"
	"treasury/verify"
)

// Server answers the routes in api/openapi.yaml.
type Server struct {
	Engine *verify.Engine
	Doc    []byte // the specification, served as itself

	// MaxImage is the largest image body accepted, in bytes. It is
	// applied before anything is decoded.
	MaxImage int64
	// MaxBatch is the largest batch body accepted, in bytes.
	MaxBatch int64
	// BatchWorkers is how many labels of a batch are read at once. Zero
	// is the core count. The engine is already parallel inside one
	// verification, so more than this oversubscribes every core and makes
	// each label slower without finishing the batch sooner.
	BatchWorkers int
	// Timeout bounds one verification. It is a context deadline, so it
	// reaches the engine rather than only the response.
	Timeout time.Duration
}

var _ api.ServerInterface = (*Server)(nil)

// Handler is the whole service: the generated routes, and nothing else.
func (s *Server) Handler() http.Handler {
	return api.HandlerFromMux(s, http.NewServeMux())
}

// Spec serves the specification this service is built from, so a caller
// can always fetch the contract the running binary was generated against
// rather than a copy that may have drifted.
func (s *Server) Spec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(s.Doc)
}

func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	id := identity()
	ready := s.Engine != nil
	h := api.Health{Ready: ready, Engine: &id}
	code := http.StatusOK
	if !ready {
		code = http.StatusServiceUnavailable
		d := "the reader's models are not loaded"
		h.Detail = &d
	}
	writeJSON(w, code, h)
}

// Verify reads one label and judges it against the claims filed for it.
func (s *Server) Verify(w http.ResponseWriter, r *http.Request) {
	if s.Engine == nil {
		fail(w, http.StatusServiceUnavailable, "The service is not ready to read images yet.")
		return
	}
	// The body is bounded before anything is decoded, so an oversized
	// upload costs the bandwidth to notice it and nothing else.
	max := s.MaxImage
	if max <= 0 {
		max = 10 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, max+(1<<20))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("The image is larger than this service accepts, which is %d MB.", max>>20))
			return
		}
		fail(w, http.StatusBadRequest, "The request was not a valid multipart form: send an image and a claims field.")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll() // nothing is kept
		}
	}()

	file, header, err := r.FormFile("image")
	if err != nil {
		fail(w, http.StatusBadRequest, "No image was sent. Attach the label as the 'image' field.")
		return
	}
	defer file.Close()
	if header.Size > max {
		fail(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("The image is %d MB and this service accepts %d MB.", header.Size>>20, max>>20))
		return
	}
	raw := r.FormValue("claims")
	if raw == "" {
		if f, _, err := r.FormFile("claims"); err == nil {
			defer f.Close()
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(f); err == nil {
				raw = buf.String()
			}
		}
	}
	if raw == "" {
		fail(w, http.StatusBadRequest, "No claims were sent. Attach the filed application as the 'claims' field, as JSON.")
		return
	}
	var app api.Application
	if err := json.Unmarshal([]byte(raw), &app); err != nil {
		fail(w, http.StatusBadRequest, "The claims field was not valid JSON: "+err.Error())
		return
	}
	img, _, err := image.Decode(file)
	if err != nil {
		fail(w, http.StatusBadRequest, "The image could not be read. Send a PNG or a JPEG.")
		return
	}

	ctx := r.Context()
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	refs, claims := ttb.Inputs(expectedOf(app))
	start := time.Now()
	res, err := s.Engine.Verify(ctx, img, refs, claims)
	took := time.Since(start)
	if err != nil {
		if ctx.Err() != nil {
			fail(w, http.StatusGatewayTimeout, "The label took too long to read and the request was stopped.")
			return
		}
		fail(w, http.StatusInternalServerError, "The label could not be verified: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resultOf(res, took, img))
}

// expectedOf turns the filed application as it arrives over the wire into
// the domain's own type. It is a translation and nothing more: no field
// is defaulted or inferred here, because a claim the caller did not file
// is not a claim this service may invent.
func expectedOf(a api.Application) ttb.Expected {
	e := ttb.Expected{Beverage: string(a.Beverage)}
	if a.Brand != nil {
		e.Brand = *a.Brand
	}
	if a.Class != nil {
		e.Class = *a.Class
	}
	if a.Producer != nil {
		e.Producer = *a.Producer
	}
	if a.Origin != nil {
		e.Origin = *a.Origin
	}
	if a.Abv != nil {
		e.ABV = *a.Abv
	}
	if a.NetMl != nil {
		e.NetML = *a.NetMl
	}
	if a.Aliases != nil {
		e.Aliases = map[string][]string{}
		for k, v := range *a.Aliases {
			e.Aliases[k] = v
		}
	}
	return e
}

func identity() api.Identity {
	id := buildid.Get()
	models := map[string]string{}
	for k, v := range id.Models {
		models[k] = v
	}
	return api.Identity{
		Version: &id.Version, Commit: &id.Commit, CommitTime: &id.CommitTime,
		Modified: &id.Modified, Go: &id.Go, Models: &models,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// fail answers with a sentence a person can act on. The reader of this
// service is a reviewer, not a programmer, so an error says what to do
// rather than what went wrong internally.
func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, api.Error{Error: msg})
}

// cropPNG cuts the evidence region out of the image, so a reviewer sees
// what the engine saw rather than taking its word for it.
func cropPNG(img image.Image, box image.Rectangle) []byte {
	b := box.Intersect(img.Bounds())
	if b.Empty() {
		return nil
	}
	// A little air around it, so the region reads as part of a label
	// rather than as a strip of letters.
	pad := b.Dy() / 3
	b = b.Inset(-pad).Intersect(img.Bounds())
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	var out image.Image
	if ok {
		out = sub.SubImage(b)
	} else {
		dst := image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				dst.Set(x, y, img.At(x, y))
			}
		}
		out = dst
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil
	}
	return buf.Bytes()
}
