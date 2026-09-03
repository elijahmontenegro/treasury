// Command fetchfonts downloads the bundled OFL faces into internal/render/fonts
// together with their licences. It runs once; the files are committed.
//
//	fetchfonts [-out internal/render/fonts]
//
// Only families that still ship static regular and bold TTFs are listed:
// x/image renders a variable font at its default instance only.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const base = "https://raw.githubusercontent.com/google/fonts/main/ofl/"

var fonts = []struct{ dir, file, name string }{
	{"lato", "Lato-Regular.ttf", "Lato-Regular.ttf"},
	{"lato", "Lato-Bold.ttf", "Lato-Bold.ttf"},
	{"ptsans", "PT_Sans-Web-Regular.ttf", "PTSans-Regular.ttf"},
	{"ptsans", "PT_Sans-Web-Bold.ttf", "PTSans-Bold.ttf"},
	{"ptserif", "PT_Serif-Web-Regular.ttf", "PTSerif-Regular.ttf"},
	{"ptserif", "PT_Serif-Web-Bold.ttf", "PTSerif-Bold.ttf"},
	{"fjallaone", "FjallaOne-Regular.ttf", "FjallaOne-Regular.ttf"},
	{"anton", "Anton-Regular.ttf", "Anton-Regular.ttf"},
	{"abrilfatface", "AbrilFatface-Regular.ttf", "AbrilFatface-Regular.ttf"},
	{"lobster", "Lobster-Regular.ttf", "Lobster-Regular.ttf"},
	{"alfaslabone", "AlfaSlabOne-Regular.ttf", "AlfaSlabOne-Regular.ttf"},
}

func main() {
	out := flag.String("out", filepath.Join("internal", "render", "fonts"), "output directory")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "fetchfonts:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var licenses strings.Builder
	licenses.WriteString("# Bundled font licences\n\nEvery face in this directory is distributed under the SIL Open Font License 1.1, reproduced per family below as shipped in the google/fonts repository.\n")
	seen := map[string]bool{}
	for _, f := range fonts {
		b, err := get(base + f.dir + "/" + f.file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), b, 0o644); err != nil {
			return err
		}
		fmt.Printf("%s (%d bytes)\n", f.name, len(b))
		if seen[f.dir] {
			continue
		}
		seen[f.dir] = true
		lic, err := get(base + f.dir + "/OFL.txt")
		if err != nil {
			return err
		}
		fmt.Fprintf(&licenses, "\n## %s\n\n```\n%s\n```\n", f.dir, strings.TrimSpace(string(lic)))
	}
	return os.WriteFile(filepath.Join(dir, "LICENSES.md"), []byte(licenses.String()), 0o644)
}

func get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
