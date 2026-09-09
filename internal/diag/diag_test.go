package diag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name string, first any) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(first); err != nil {
		t.Fatal(err)
	}
	// A record of the kind that follows a header, so the file looks like
	// a real diagnostic rather than a lone line.
	if err := json.NewEncoder(f).Encode(map[string]any{"label": "0001", "claims": []any{}}); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAFileWithoutProvenanceIsRefused is step 26c's own case. It scored
// out/why24b.jsonl, a diagnostic written at step 24b before step 25a
// widened the radius, and reported nine names where eight was the number.
// Every diagnostic written before step 28b looks like this: a bare list of
// records saying nothing about which engine produced it.
func TestAFileWithoutProvenanceIsRefused(t *testing.T) {
	p := write(t, t.TempDir(), "why24b.jsonl",
		map[string]any{"label": "0002", "regions": 34, "claims": []any{}})
	err := Check(p)
	if err == nil {
		t.Fatal("a diagnostic with no provenance was accepted; step 26c's case would " +
			"have gone unnoticed again")
	}
	if !strings.Contains(err.Error(), "no provenance") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// TestAFileOlderThanTheEngineIsRefused is the case the fingerprint alone
// cannot catch: the same commit with the tree modified, which is what
// every measuring run during an amendment looks like.
func TestAFileOlderThanTheEngineIsRefused(t *testing.T) {
	h, err := Stamp()
	if err != nil {
		t.Fatal(err)
	}
	// Written before the newest source file in the tree was touched,
	// which is exactly what step 25a did to step 24b's diagnostic.
	newest, _, err := newestSource(".")
	if err != nil {
		t.Fatal(err)
	}
	h.Ran = newest.Add(-time.Hour)
	p := write(t, t.TempDir(), "stale.jsonl", h)
	err = Check(p)
	if err == nil {
		t.Fatal("a diagnostic written before the engine changed was accepted")
	}
	if !strings.Contains(err.Error(), "changed after this file was written") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// TestACurrentFileIsAccepted keeps the check from being one that refuses
// everything, which would be useless in the other direction.
func TestACurrentFileIsAccepted(t *testing.T) {
	h, err := Stamp()
	if err != nil {
		t.Fatal(err)
	}
	h.Ran = time.Now().UTC().Add(time.Minute)
	p := write(t, t.TempDir(), "fresh.jsonl", h)
	if err := Check(p); err != nil {
		t.Errorf("a diagnostic from the current engine was refused: %v", err)
	}
}
