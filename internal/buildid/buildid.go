// Package buildid reports what a binary is: which commit it was built
// from, whether the tree was clean, and the exact weights it carries.
//
// A verdict is an assertion about a label, and an assertion is worth what
// its provenance is worth. The commit and its time come from the Go tool
// chain's own build information, so two builds of one commit report the
// same identity where a wall-clock build date would not. The model hashes
// are computed from the embedded bytes the binary actually runs, not
// stamped alongside them, so a rebuilt model cannot report an old hash.
package buildid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
)

// Version is the release name, set with -ldflags "-X treasury/internal/buildid.Version=..."
// when a release is cut; otherwise the commit identifies the build.
var Version = "dev"

// Identity is what produced a verdict.
type Identity struct {
	Version    string            `json:"version"`
	Commit     string            `json:"commit"`
	CommitTime string            `json:"commit_time"`
	Modified   bool              `json:"modified"` // the tree had uncommitted changes
	Go         string            `json:"go"`
	Models     map[string]string `json:"models"` // name to SHA-256 of the bytes the binary carries
}

var (
	once   sync.Once
	id     Identity
	models = map[string][]byte{}
	mu     sync.Mutex
)

// Register records a model's bytes under a name. Each package that embeds
// weights calls this from its own initialization, so the identity covers
// exactly what the binary carries.
func Register(name string, data []byte) {
	mu.Lock()
	defer mu.Unlock()
	sum := sha256.Sum256(data)
	models[name] = sum[:]
}

// Get returns the identity of this binary.
func Get() Identity {
	once.Do(func() {
		id = Identity{Version: Version, Commit: "unknown", Models: map[string]string{}}
		if info, ok := debug.ReadBuildInfo(); ok {
			id.Go = info.GoVersion
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					id.Commit = s.Value
				case "vcs.time":
					id.CommitTime = s.Value
				case "vcs.modified":
					id.Modified = s.Value == "true"
				}
			}
		}
		mu.Lock()
		for name, sum := range models {
			id.Models[name] = hex.EncodeToString(sum)
		}
		mu.Unlock()
	})
	return id
}

// Fingerprint is a short digest of the identity: the commit, whether the
// tree was modified, and every model hash. A verdict carries it so that a
// verdict separated from its result is still traceable to the weights that
// produced it.
func Fingerprint() string {
	i := Get()
	names := make([]string, 0, len(i.Models))
	for n := range i.Models {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%v", i.Version, i.Commit, i.Modified)
	for _, n := range names {
		fmt.Fprintf(h, "\x00%s=%s", n, i.Models[n])
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// String is the identity in one line, for a command's banner.
func (i Identity) String() string {
	commit := i.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	if i.Modified {
		commit += "+modified"
	}
	names := make([]string, 0, len(i.Models))
	for n := range i.Models {
		names = append(names, n)
	}
	sort.Strings(names)
	var parts []string
	for _, n := range names {
		parts = append(parts, n+" "+i.Models[n][:12])
	}
	s := fmt.Sprintf("treasury %s %s (%s, %s)", i.Version, commit, i.CommitTime, i.Go)
	if len(parts) > 0 {
		s += ", models: " + strings.Join(parts, ", ")
	}
	return s + ", fingerprint " + Fingerprint()
}
