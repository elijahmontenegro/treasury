package api_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// TestGeneratedCodeIsCurrent fails when api/openapi.yaml has been edited
// without regenerating, so the specification a caller reads and the types
// this service answers with cannot drift apart quietly.
//
// It is skipped where the generator cannot be fetched, and says so, since
// a machine with no network is not a machine with a stale spec.
func TestGeneratedCodeIsCurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("fetches the generator")
	}
	want, err := os.ReadFile("api.gen.go")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "api.gen.go")
	// The real configuration with only its output redirected, so the
	// generate options stay in one place and this cannot drift from what
	// `go generate` does.
	cfg, err := os.ReadFile("cfg.yaml")
	if err != nil {
		t.Fatal(err)
	}
	redirected := regexp.MustCompile(`(?m)^output:.*$`).
		ReplaceAllString(string(cfg), "output: "+filepath.ToSlash(out))
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte(redirected), 0o644); err != nil {
		t.Fatal(err)
	}
	spec, err := filepath.Abs("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run",
		"github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1",
		"-config", cfgPath, spec)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("the generator could not be run here: %v\n%s", err, b)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("api/api.gen.go is not what openapi.yaml generates. " +
			"Run `go generate ./api/` and commit the result.")
	}
}
