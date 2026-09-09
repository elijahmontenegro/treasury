// Command decode verifies one image against an application and prints the
// verdicts.
//
//	decode -ttb eval/real50/0047.json eval/real50/0047.png
//	decode -version
//
// It is the CLI over the engine: the image goes in as uploaded, the reader
// finds and reads the text, and the decision layer judges each claim
// against what was read.
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

	"treasury/internal/buildid"
	"treasury/ttb"
	"treasury/verify"
)

func main() {
	exp := flag.String("ttb", "", "the application as JSON: brand, class, producer, origin, abv, net_ml")
	version := flag.Bool("version", false, "print the build identity and exit")
	regions := flag.Bool("regions", false, "print the regions the reader found, with their text")
	flag.Parse()
	if *version {
		b, err := json.MarshalIndent(buildid.Get(), "", " ")
		if err != nil {
			fail(err)
		}
		fmt.Println(string(b))
		return
	}
	if flag.NArg() != 1 || *exp == "" {
		fmt.Fprintln(os.Stderr, "usage: decode -ttb application.json image.png")
		os.Exit(2)
	}
	if err := run(*exp, flag.Arg(0), *regions); err != nil {
		fail(err)
	}
}

func run(expPath, imgPath string, regions bool) error {
	b, err := os.ReadFile(expPath)
	if err != nil {
		return err
	}
	var exp ttb.Expected
	if err := json.Unmarshal(b, &exp); err != nil {
		return err
	}
	f, err := os.Open(imgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		return err
	}
	refs, claims := ttb.Inputs(exp)
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		return err
	}
	if regions {
		for _, r := range res.Regions {
			fmt.Printf("%v %.2f %q\n", r.Box, r.Confidence, r.Text)
		}
		return nil
	}
	out, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "decode:", err)
	os.Exit(1)
}
