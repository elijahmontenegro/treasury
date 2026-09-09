package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"treasury/api"
	"treasury/internal/httpapi"
	"treasury/ttb"
	"treasury/verify"
)

// sample is a real label and the application filed for it.
const sample = "0047"

func labelPath(t *testing.T, ext string) string {
	t.Helper()
	p := filepath.Join("..", "..", "eval", "real50", sample+ext)
	if _, err := os.Stat(p); err != nil {
		needData(t, p+" is not in the tree")
	}
	return p
}

// TestOverHTTPMatchesTheEngine is step 30b's gate: the same label sent to
// the service must come back with the verdicts the engine gives directly,
// byte for byte.
//
// The comparison is on the verdicts and their evidence rather than on the
// whole response, because the response carries two things the engine does
// not: how long the call took, and a PNG of each region. Those are
// checked for presence instead — a response whose evidence has no crop is
// not what this service promises.
func TestOverHTTPMatchesTheEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("long: loads the reader and verifies a label twice")
	}
	imgPath := labelPath(t, ".png")
	appPath := labelPath(t, ".json")

	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()

	raw, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatal(err)
	}
	var exp ttb.Expected
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatal(err)
	}

	// What the engine says, called directly the way the CLI calls it.
	f, err := os.Open(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	refs, claims := ttb.Inputs(exp)
	want, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		t.Fatal(err)
	}

	// What the service says, over the wire.
	srv := &httpapi.Server{Engine: eng, MaxImage: 10 << 20}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", sample+".png")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(src); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("claims", string(raw)); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	resp, err := http.Post(ts.URL+"/verify", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io_ReadAll(resp)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var got api.Result
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	if len(got.Claims) != len(want.Claims) {
		t.Fatalf("%d claims over HTTP, %d from the engine", len(got.Claims), len(want.Claims))
	}
	crops := 0
	for i, w := range want.Claims {
		g := got.Claims[i]
		if g.Claim != w.Claim || string(g.Status) != w.Status.String() || g.Expected != w.Expected {
			t.Errorf("claim %d: %s/%s/%q over HTTP, %s/%s/%q from the engine",
				i, g.Claim, g.Status, g.Expected, w.Claim, w.Status, w.Expected)
			continue
		}
		if (g.Evidence == nil) != (w.Evidence == nil) {
			t.Errorf("%s: evidence present over HTTP=%v, from the engine=%v",
				w.Claim, g.Evidence != nil, w.Evidence != nil)
			continue
		}
		if w.Evidence == nil {
			continue
		}
		if str(g.Evidence.Read) != w.Evidence.Read || str(g.Evidence.Matched) != w.Evidence.Matched {
			t.Errorf("%s: read %q/%q, matched %q/%q", w.Claim,
				str(g.Evidence.Read), w.Evidence.Read,
				str(g.Evidence.Matched), w.Evidence.Matched)
		}
		got, sent := f64(g.Evidence.Distance)
		if !sent {
			t.Errorf("%s: the response carried no distance, and the engine measured %v; "+
				"an exact match is evidence, not an absent field", w.Claim, w.Evidence.Distance)
		} else if got != w.Evidence.Distance {
			t.Errorf("%s: distance %v over HTTP, %v from the engine",
				w.Claim, got, w.Evidence.Distance)
		}
		if r, sent := f64(g.Evidence.Radius); !sent || r != w.Evidence.Radius {
			t.Errorf("%s: radius %v/%v over HTTP, %v from the engine", w.Claim, r, sent, w.Evidence.Radius)
		}
		if g.Evidence.Crop != nil && len(*g.Evidence.Crop) > 0 {
			crops++
		}
	}
	if crops == 0 {
		t.Error("no verdict carried an evidence crop; the service promises one for every region")
	}
	if got.Engine.Commit == nil || *got.Engine.Commit == "" {
		t.Error("the response carried no build identity")
	}
}

// TestHealthReportsTheBuild keeps /health from becoming a bare 200: what
// it is for is saying which build is answering.
func TestHealthReportsTheBuild(t *testing.T) {
	srv := &httpapi.Server{}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// No engine, so not ready, and it says so rather than lying.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d with no engine, want 503", resp.StatusCode)
	}
	var h api.Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if h.Ready {
		t.Error("reported ready with no engine")
	}
	if h.Engine == nil || h.Engine.Go == nil || *h.Engine.Go == "" {
		t.Error("no build identity in the health response")
	}
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// f64 was the reason nothing saw finding 3. It mapped a missing field
// back to zero, so a distance the service had dropped compared equal to
// the zero the engine reported and the gate passed. What a comparison
// needs here is the difference between "zero" and "not sent", so the
// caller is told which it got.
func f64(p *float64) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return *p, true
}

func io_ReadAll(r *http.Response) ([]byte, error) {
	var b bytes.Buffer
	_, err := b.ReadFrom(r.Body)
	return b.Bytes(), err
}

// TestThePageIsServed keeps the operator's screen from silently becoming
// a 404: it is served by the same binary, from an embedded file, so that
// it works wherever the service does.
func TestThePageIsServed(t *testing.T) {
	srv := &httpapi.Server{}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d for the page", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	b, _ := io_ReadAll(resp)
	// No external asset may be referenced: the page has to work where the
	// service does, which may be somewhere that cannot reach a CDN.
	for _, bad := range []string{"http://", "https://", "//cdn", "src=\"//"} {
		if bytes.Contains(b, []byte(bad)) {
			t.Errorf("the page references something outside the binary: %q", bad)
		}
	}
	for _, want := range []string{"Check this label", "Brand name", "Net contents"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("the page does not offer %q", want)
		}
	}
}
