// Package diag stamps a diagnostic file with the engine that produced it,
// and refuses one that a later engine is being scored against.
//
// Step 26c scored `out/why24b.jsonl`, written at step 24b, and reported
// nine names refused for want of a delimiter where eight was the number:
// step 25a had widened the radius in between and recovered one of them.
// Step 27a found it. The file was reused rather than regenerated, which
// nothing in the build could have noticed, because a diagnostic was a
// bare list of records that said nothing about where it came from.
//
// So a diagnostic now carries its provenance in its first line and the
// scorer refuses a stale one by name. Two things are checked and they
// catch different mistakes. The build fingerprint covers the commit and
// the exact model weights, so it catches a file written by a different
// version. The source's own modification time catches what the
// fingerprint cannot: during development the tree is nearly always
// modified, and two builds of a modified tree report the same identity,
// so a file written before the newest Go file in the tree was touched is
// refused as well. Comparing binaries would be the wrong test - the
// checker and the diagnostic are different programs, built at different
// moments for reasons that say nothing about the engine.
package diag

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"treasury/internal/buildid"
)

// Header is the first record of a diagnostic file.
type Header struct {
	// Diagnostic is present only on this record, so a reader can tell the
	// header from the records that follow it without counting lines.
	Diagnostic bool `json:"diagnostic"`

	Fingerprint string           `json:"fingerprint"`
	Engine      buildid.Identity `json:"engine"`
	// BinaryTime is when the binary that wrote this file was last built.
	BinaryTime time.Time `json:"binary_time"`
	Ran        time.Time `json:"ran"`
}

// Stamp builds the header for the running binary.
func Stamp() (Header, error) {
	h := Header{Diagnostic: true, Fingerprint: buildid.Fingerprint(),
		Engine: buildid.Get(), Ran: time.Now().UTC()}
	exe, err := os.Executable()
	if err != nil {
		return h, err
	}
	st, err := os.Stat(exe)
	if err != nil {
		return h, err
	}
	h.BinaryTime = st.ModTime().UTC()
	return h, nil
}

// Check reads the first line of a diagnostic file and reports why it may
// not be scored against the running binary, or nil if it may.
func Check(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var h Header
	if err := json.NewDecoder(f).Decode(&h); err != nil {
		return fmt.Errorf("%s: unreadable: %v", path, err)
	}
	if !h.Diagnostic {
		return fmt.Errorf("%s: no provenance: this file was written before step 28b, "+
			"so there is no way to tell which engine produced it and it may not be scored",
			path)
	}
	now, err := Stamp()
	if err != nil {
		return err
	}
	if h.Fingerprint != now.Fingerprint {
		return fmt.Errorf("%s: written by engine %s, scored against %s: "+
			"regenerate it from the current binary",
			path, h.Fingerprint, now.Fingerprint)
	}
	// The fingerprint cannot separate two builds of a modified tree, and
	// during development the tree is nearly always modified, so the
	// second test is against the source rather than against a binary:
	// if any Go file changed after the file was written, the engine that
	// wrote it is not the engine in the tree. Comparing binaries would
	// be wrong here - the checker and the diagnostic are different
	// programs, built at different moments for reasons that say nothing
	// about the engine.
	newest, which, err := newestSource(".")
	if err != nil {
		return err
	}
	if h.Ran.Before(newest) {
		return fmt.Errorf("%s: written %s, and %s changed at %s: "+
			"the engine changed after this file was written, so regenerate it",
			path, h.Ran.Format(time.RFC3339), which, newest.Format(time.RFC3339))
	}
	return nil
}

// newestSource is the most recently modified Go file under root, and its
// path. Generated output is skipped: `out` holds the binaries and the
// scoring scripts, which change constantly and are not the engine.
func newestSource(root string) (time.Time, string, error) {
	var newest time.Time
	var which string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "out", ".git", "synth", "real2", "third_party", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return nil
		}
		if st.ModTime().After(newest) {
			newest, which = st.ModTime().UTC(), filepath.ToSlash(p)
		}
		return nil
	})
	return newest, which, err
}
