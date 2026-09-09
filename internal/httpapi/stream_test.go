package httpapi_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"treasury/api"
	"treasury/internal/httpapi"
	"treasury/verify"
)

// serviceFor assembles the service the way cmd/serve does, through
// httpapi.Service, because a test that calls Server.Handler directly
// cannot see anything the middleware chain does — which is how a wrapper
// that swallowed http.Flusher went unnoticed while the README, the
// specification and the record all said the batch streamed.
func serviceFor(t *testing.T, eng *verify.Engine, lim httpapi.Limits, workers int) *httptest.Server {
	t.Helper()
	srv := &httpapi.Server{Engine: eng, MaxBatch: 400 << 20, Limits: lim, BatchWorkers: workers}
	ts := httptest.NewServer(httpapi.Service(srv, nil, lim, httpapi.NewBudget(lim.DailySeconds)))
	t.Cleanup(ts.Close)
	return ts
}

// TestTheBatchStreams is the claim three documents make and nothing
// checked: a line is written as each label finishes, not when the batch
// does.
//
// It is measured rather than asserted, because "it streamed" has no
// meaning on its own. The first line's arrival is compared to the whole
// response's: if the answer is assembled and sent at the end, the two are
// the same moment, and with a handful of labels they are seconds apart.
func TestTheBatchStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("long: verifies several labels")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()

	// One worker, so the labels finish one after another rather than all
	// at once. With as many workers as labels they all start together and
	// the first cannot finish early however well the stream works, which
	// makes the measurement say nothing.
	const n = 5
	csvBytes, zipBytes, _ := batchOf(t, n)
	ts := serviceFor(t, eng, httpapi.DefaultLimits(), 1)

	start := time.Now()
	resp := postBatch(t, ts.URL, csvBytes, zipBytes)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	var firstLine, lastLine time.Duration
	lines := 0
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		lines++
		if lines == 1 {
			firstLine = time.Since(start)
		}
		lastLine = time.Since(start)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != n {
		t.Fatalf("%d lines for %d labels", lines, n)
	}
	t.Logf("first line at %s, last at %s", firstLine.Round(time.Millisecond),
		lastLine.Round(time.Millisecond))

	// One worker and five labels: the first line is one label's work in
	// and the last is five, so streaming puts the first at about a fifth
	// of the last. Half is the bound, which is wide enough that a slow
	// first label does not fail it and tight enough to rule out what was
	// happening — every line arriving at the end, in one piece.
	if firstLine > lastLine/2 {
		t.Errorf("the first line arrived at %s and the last at %s, with one worker and %d labels: "+
			"the answer was assembled rather than streamed",
			firstLine.Round(time.Millisecond), lastLine.Round(time.Millisecond), n)
	}
}

// TestALongBatchIsRefusedRatherThanTruncated is the other half of the
// same defect. A batch used to be admitted, answered 200, and then cut
// off mid-stream by the server's write timeout, with no way for the
// caller to tell a truncated answer from a whole one.
//
// Two things are required now, and they are the same requirement from
// either side: a batch the service cannot deliver is refused before it
// starts, and one it admits is delivered whole even when it takes longer
// than a single verification is allowed to.
func TestALongBatchIsRefusedRatherThanTruncated(t *testing.T) {
	if testing.Short() {
		t.Skip("long: verifies a batch")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		needReader(t, err)
	}
	defer eng.Close()

	t.Run("more than can be delivered is refused before it starts", func(t *testing.T) {
		lim := httpapi.DefaultLimits()
		lim.BatchWall = 10 * time.Second
		lim.PerLabel = 2 * time.Second
		lim.MaxRows = int(lim.BatchWall / lim.PerLabel) // five
		csvBytes, zipBytes, _ := batchOf(t, 12)
		ts := serviceFor(t, eng, lim, 0)
		resp := postBatch(t, ts.URL, csvBytes, zipBytes)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d for twelve labels against a bound of five, want 413", resp.StatusCode)
		}
		var e api.Error
		json.NewDecoder(resp.Body).Decode(&e)
		if !strings.Contains(e.Error, "5") {
			t.Errorf("the refusal does not say what the bound is: %q", e.Error)
		}
	})

	// A batch that outlives what one verification is allowed. The server
	// under test is given a write timeout shorter than the batch will
	// take, which is exactly the shape that truncated it: at the shipped
	// defaults, ninety seconds against a batch of minutes.
	t.Run("longer than a single verification is delivered whole", func(t *testing.T) {
		const n = 8
		csvBytes, zipBytes, names := batchOf(t, n)
		lim := httpapi.DefaultLimits()
		srv := &httpapi.Server{Engine: eng, MaxBatch: 400 << 20, Limits: lim}
		ts := httptest.NewUnstartedServer(httpapi.Service(srv, nil, lim, nil))
		// Shorter than the batch takes. Without the stream's own
		// deadline this is what cut it off.
		ts.Config.WriteTimeout = 3 * time.Second
		ts.Start()
		defer ts.Close()

		resp := postBatch(t, ts.URL, csvBytes, zipBytes)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
		seen := map[string]bool{}
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var item api.BatchItem
			if err := json.Unmarshal([]byte(line), &item); err != nil {
				t.Fatalf("a line was not whole: %v", err)
			}
			seen[item.Image] = true
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("the stream ended badly: %v", err)
		}
		for _, name := range names {
			if !seen[name] {
				t.Errorf("%s never came back: the response was truncated with a 200 already sent", name)
			}
		}
	})
}

// TestTheBoundsAgree pins the arithmetic that finding 2 was: three
// numbers set in three places that did not agree, so a batch inside the
// row bound was cut off by the write timeout with a 200 already sent.
//
// MaxRows is not a preference. It is what BatchWall allows at PerLabel,
// and if it is ever set to something else by hand this says so.
func TestTheBoundsAgree(t *testing.T) {
	lim := httpapi.DefaultLimits()
	if lim.PerLabel <= 0 || lim.BatchWall <= 0 {
		t.Fatalf("the two the bound is derived from are unset: PerLabel %s, BatchWall %s",
			lim.PerLabel, lim.BatchWall)
	}
	allowed := int(lim.BatchWall / lim.PerLabel)
	if lim.MaxRows != allowed {
		t.Errorf("MaxRows is %d, but %s at %s a label allows %d: "+
			"the largest batch accepted must be one that can also be delivered",
			lim.MaxRows, lim.BatchWall, lim.PerLabel, allowed)
	}
	// And the gate's own three hundred has to fit, or the thing this
	// service is measured on is a thing it would refuse.
	if lim.MaxRows < 300 {
		t.Errorf("MaxRows is %d and the batch gate sends 300", lim.MaxRows)
	}
}
