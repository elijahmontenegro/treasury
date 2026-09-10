package httpapi_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"treasury/api"
	"treasury/internal/httpapi"
	"treasury/verify"
)

// A batch is the cheapest way to ask this service for hours of work, so
// what bounds it has to be refused before the engine runs, like everything
// else in guard.go. These are the two bounds the README's claim that a
// batch "streams so nothing accumulates" was resting on and did not have.
//
// A real engine sits behind them, deliberately. With no engine the handler
// answers 503 before it looks at anything, so every case here would pass
// whether or not the bounds existed — the vacuous guard step 30a spent a
// paragraph on and step 30e repeated. With an engine loaded, a 413 naming
// the bound can only have come from the bound.
func TestABatchIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("long: loads the reader")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()

	// Deliberately small, so the test is quick and the bound is the thing
	// under test rather than the machine's patience.
	lim := httpapi.DefaultLimits()
	lim.MaxRows = 5
	lim.MaxUnzipped = 1 << 20 // 1 MB of contents

	serve := func(t *testing.T) *httptest.Server {
		t.Helper()
		srv := &httpapi.Server{Engine: eng, MaxBatch: 100 << 20, Limits: lim}
		ts := httptest.NewServer(srv.Handler())
		t.Cleanup(ts.Close)
		return ts
	}

	// A ZIP with one small entry, so the shape is valid and only the bound
	// under test can refuse it.
	oneSmallZip := func() []byte {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		f, _ := zw.Create("x.png")
		f.Write([]byte("not really a png, and never decoded"))
		zw.Close()
		return b.Bytes()
	}

	refused := func(t *testing.T, csvBytes, zipBytes []byte, what string) {
		t.Helper()
		resp := postBatch(t, serve(t).URL, csvBytes, zipBytes)
		defer resp.Body.Close()
		var e api.Error
		json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d for %s, want 413: %q", resp.StatusCode, what, e.Error)
		}
		if !strings.Contains(e.Error, "more than") {
			t.Errorf("the refusal does not say what the bound is: %q", e.Error)
		}
	}

	t.Run("more rows than a batch may ask for", func(t *testing.T) {
		var rows bytes.Buffer
		rows.WriteString("image,beverage,brand\n")
		for i := range 20 {
			fmt.Fprintf(&rows, "x%d.png,wine,PENUMBRA\n", i)
		}
		refused(t, rows.Bytes(), oneSmallZip(), "20 rows against a bound of 5")
	})

	t.Run("a ZIP that declares more than it may unpack to", func(t *testing.T) {
		// Two megabytes of zeroes: a few kilobytes on the wire, and the
		// archive's own directory says what it becomes. That is the point
		// of reading the directory rather than the body size, and it is
		// the same argument as the pixel cap.
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		f, _ := zw.Create("big.png")
		f.Write(make([]byte, 2<<20))
		zw.Close()
		if b.Len() > 64<<10 {
			t.Fatalf("the bomb is %d bytes on the wire; it should be tiny", b.Len())
		}
		refused(t, []byte("image\nbig.png\n"), b.Bytes(), "a ZIP declaring 2 MB against a bound of 1 MB")
	})

	// The other half of a bound: it has to let through what is inside it,
	// or a test that refuses everything proves nothing. This one gets all
	// the way to the stream and comes back as one NDJSON line saying the
	// entry is not an image, which is the right answer to what was sent
	// and could only be reached past both bounds.
	t.Run("a batch inside both bounds is answered", func(t *testing.T) {
		resp := postBatch(t, serve(t).URL, []byte("image,beverage\nx.png,wine\n"), oneSmallZip())
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d for a batch inside both bounds, want 200", resp.StatusCode)
		}
		var item api.BatchItem
		if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
			t.Fatalf("no line came back: %v", err)
		}
		if item.Error == nil || !strings.Contains(*item.Error, "PNG") {
			t.Errorf("the line does not say the entry is not an image: %v", item.Error)
		}
	})
}

// TestAServerWithNoLimitsStillGuards is the other half of finding 12. A
// Limits left at its zero value used to mean no pixel cap at all, so the
// guard failed open — and that was not hypothetical, it was what the
// three-hundred-label gate ran with.
func TestAServerWithNoLimitsStillGuards(t *testing.T) {
	if testing.Short() {
		t.Skip("long: loads the reader")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()

	// No Limits at all, which is the case under test.
	srv := &httpapi.Server{Engine: eng, MaxImage: 10 << 20}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ct, body := multipartBody(t, "image", "bomb.png", bomb(t),
		map[string]string{"claims": `{"beverage":"wine","brand":"X"}`})
	resp, err := http.Post(ts.URL+"/verify", ct, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d for a 60,000 by 60,000 image against a Server with no Limits, want 413: "+
			"an unset limit must fall back to the default, not to none", resp.StatusCode)
	}
	var e api.Error
	json.NewDecoder(resp.Body).Decode(&e)
	if !strings.Contains(e.Error, "60000 by 60000") {
		t.Errorf("the refusal does not name the dimensions it read: %q", e.Error)
	}
}

// TestALabelWithNoApplicationIsStillChecked is the commonest thing anyone
// would ask this service, and for a day it was refused.
//
// The statutory warning is the same text on every label and mandatory on
// all of them, so whether one carries it — exactly, in bold capitals — is
// answerable from the image alone. No application, no form, nothing
// filled in. A refusal written to stop an empty request wasting a
// verification decided that a request with no claims had nothing to
// check, which was wrong: it had the check an agent makes most often.
func TestALabelWithNoApplicationIsStillChecked(t *testing.T) {
	if testing.Short() {
		t.Skip("long: loads the reader")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()
	srv := &httpapi.Server{Engine: eng, MaxImage: 10 << 20, Limits: httpapi.DefaultLimits()}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	img, err := os.ReadFile(labelPath(t, ".png"))
	if err != nil {
		t.Fatal(err)
	}
	answered := func(t *testing.T, claims string) api.Result {
		t.Helper()
		ct, body := multipartBody(t, "image", "label.png", img,
			map[string]string{"claims": claims})
		resp, err := http.Post(ts.URL+"/verify", ct, body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			var e api.Error
			json.NewDecoder(resp.Body).Decode(&e)
			t.Fatalf("status %d for %s: %q", resp.StatusCode, claims, e.Error)
		}
		var out api.Result
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// An empty form. The warning and its heading must come back, because
	// that is the whole of what was asked and it is answerable.
	for _, claims := range []string{`{}`, `{"beverage":"wine"}`, `{"beverage":""}`} {
		t.Run("nothing filed: "+claims, func(t *testing.T) {
			out := answered(t, claims)
			if out.Reference == nil || len(*out.Reference) == 0 {
				t.Error("no verdict on the statutory warning, which needs no application")
			}
			if out.Emphasis == nil || len(*out.Emphasis) == 0 {
				t.Error("no verdict on the warning's heading")
			}
		})
	}

	// And the two that genuinely cannot be answered. A claim needs a
	// beverage, because what a label must say depends on it; and a
	// beverage this service does not know is a mistake worth naming
	// rather than ignoring.
	for _, c := range []struct{ name, claims, says string }{
		{"a claim with no beverage", `{"brand":"X"}`, "beverage"},
		{"a beverage this service does not know", `{"beverage":"mead","brand":"X"}`, "mead"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ct, body := multipartBody(t, "image", "label.png", img,
				map[string]string{"claims": c.claims})
			resp, err := http.Post(ts.URL+"/verify", ct, body)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d for %s, want 400", resp.StatusCode, c.claims)
			}
			var e api.Error
			json.NewDecoder(resp.Body).Decode(&e)
			if !strings.Contains(strings.ToLower(e.Error), c.says) {
				t.Errorf("the refusal does not say what is wrong: %q", e.Error)
			}
		})
	}
}
