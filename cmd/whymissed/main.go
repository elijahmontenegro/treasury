// Command whymissed reports, for every claim of every label given, the
// verdict and the probes that say where the claim was lost (step 20a).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"treasury/ttb"
	"treasury/verify"
)

func main() {
	flag.Parse()
	eng, err := verify.New(verify.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer eng.Close()
	enc := json.NewEncoder(os.Stdout)
	for _, path := range flag.Args() {
		if err := one(eng, enc, path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		}
	}
}

func one(eng *verify.Engine, enc *json.Encoder, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	var exp ttb.Expected
	b, err := os.ReadFile(base + ".json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &exp); err != nil {
		return err
	}
	refs, claims := ttb.Inputs(exp)
	regions, diag, frame, err := eng.Diagnose(context.Background(), img, refs, claims)
	if err != nil {
		return err
	}
	return enc.Encode(struct {
		Label   string             `json:"label"`
		Regions int                `json:"regions"`
		Frame   verify.Frame       `json:"frame"`
		Claims  []verify.Diagnosis `json:"claims"`
	}{filepath.Base(base), len(regions), frame, diag})
}
