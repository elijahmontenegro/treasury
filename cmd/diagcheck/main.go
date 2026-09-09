// Command diagcheck refuses a diagnostic file that a later engine is
// being scored against (step 28b).
//
//	diagcheck out/why27a.jsonl && python out/the24.py
//
// It exits non-zero and names the file, so a scoring step chained after
// it cannot run on stale input.
package main

import (
	"flag"
	"fmt"
	"os"

	"treasury/internal/diag"
	// The fingerprint covers the model weights the binary carries, so a
	// checker that carried none would compute a different one from the
	// engine and refuse every file. It is imported for that effect only.
	_ "treasury/internal/ocr"
)

func main() {
	flag.Parse()
	bad := 0
	for _, p := range flag.Args() {
		if err := diag.Check(p); err != nil {
			fmt.Fprintln(os.Stderr, "diagcheck:", err)
			bad++
			continue
		}
		fmt.Printf("%s: current\n", p)
	}
	if bad > 0 {
		os.Exit(1)
	}
}
