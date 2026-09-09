package httpapi_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"treasury/api"
	"treasury/internal/httpapi"
	"treasury/verify"
)

// batchOf builds a CSV and a ZIP naming the fifty's labels, repeated
// until there are n rows, so a batch of three hundred can be sent without
// three hundred distinct labels being in the tree.
func batchOf(t *testing.T, n int) (csv []byte, zipped []byte, names []string) {
	t.Helper()
	dir := filepath.Join("..", "..", "eval", "real50")
	entries, err := os.ReadDir(dir)
	if err != nil {
		needData(t, "eval/real50 is not in the tree")
		return nil, nil, nil
	}
	var pngs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".png") {
			pngs = append(pngs, e.Name())
		}
	}
	if len(pngs) == 0 {
		needData(t, "no labels in eval/real50")
		return nil, nil, nil
	}

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	var rows bytes.Buffer
	rows.WriteString("image,beverage,brand,class,producer,origin,abv,net_ml\n")
	for i := range n {
		src := pngs[i%len(pngs)]
		name := fmt.Sprintf("%03d_%s", i, src)
		raw, err := os.ReadFile(filepath.Join(dir, src))
		if err != nil {
			t.Fatal(err)
		}
		// Stored rather than deflated: these are PNGs, so compressing
		// them again buys nothing and costs the test seconds.
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(raw); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&rows, "%s,wine,PENUMBRA,VODKA,Valley Mill Company,PRODUCT OF USA,40,750\n", name)
		names = append(names, name)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return rows.Bytes(), zbuf.Bytes(), names
}

func postBatch(t *testing.T, url string, csvBytes, zipBytes []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	c, err := mw.CreateFormFile("claims", "claims.csv")
	if err != nil {
		t.Fatal(err)
	}
	c.Write(csvBytes)
	z, err := mw.CreateFormFile("images", "images.zip")
	if err != nil {
		t.Fatal(err)
	}
	z.Write(zipBytes)
	mw.Close()
	resp, err := http.Post(url+"/verify/batch", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestBatchCompletesEveryItem is the batch gate: three hundred items all
// come back, one line each, as they finish, and memory does not grow with
// the size of the batch.
func TestBatchCompletesEveryItem(t *testing.T) {
	if testing.Short() {
		t.Skip("long: verifies three hundred labels")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()

	const n = 300
	csvBytes, zipBytes, names := batchOf(t, n)
	srv := &httpapi.Server{Engine: eng, MaxBatch: 400 << 20}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	resp := postBatch(t, ts.URL, csvBytes, zipBytes)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io_ReadAll(resp)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content type %q, want application/x-ndjson", ct)
	}

	seen := map[string]bool{}
	lines := 0
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var item api.BatchItem
		if err := json.Unmarshal(line, &item); err != nil {
			t.Fatalf("line %d is not JSON: %v", lines+1, err)
		}
		lines++
		seen[item.Image] = true
		if item.Error != nil {
			t.Errorf("%s: %s", item.Image, *item.Error)
			continue
		}
		if item.Claims == nil || len(*item.Claims) == 0 {
			t.Errorf("%s came back with no verdicts", item.Image)
		}
		// A batch carries no crops, only whether one exists.
		for _, v := range *item.Claims {
			if v.Evidence != nil && v.Evidence.Crop != nil {
				t.Fatalf("%s carried a crop in a batch response", item.Image)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != n {
		t.Errorf("%d lines for %d items", lines, n)
	}
	for _, name := range names {
		if !seen[name] {
			t.Errorf("%s never came back", name)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&after)
	// Flat means the footprint is the images in flight, not the batch:
	// three hundred labels average about 2 MB each, so holding them all
	// would be several hundred megabytes. A hundred is generous room for
	// the reader's own arenas and still far below that.
	grew := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if grew > 100<<20 {
		t.Errorf("the heap grew by %d MB over a %d-item batch; it should not grow with the batch",
			grew>>20, n)
	}
	t.Logf("%d items, heap %+d MB", lines, grew>>20)
}

// TestBatchSaysWhatIsWrong keeps the refusals in plain language, and
// keeps a bad row from taking the batch down with it.
func TestBatchSaysWhatIsWrong(t *testing.T) {
	srv := &httpapi.Server{Engine: nil}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	// No engine: the whole batch is refused before anything is read.
	resp := postBatch(t, ts.URL, []byte("image\nx.png\n"), []byte("not a zip"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d with no engine, want 503", resp.StatusCode)
	}
	var e api.Error
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(e.Error), ".") {
		t.Errorf("the refusal is not a sentence: %q", e.Error)
	}
}
