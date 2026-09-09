package httpapi_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
