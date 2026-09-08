package verify_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"

	"treasury/internal/render"
	"treasury/internal/synth"
	"treasury/ttb"
	"treasury/verify"
)

// The engine must give the same answer about the same image every time, in
// any process. It did not: three sums ran in Go map order, which is
// randomized per process and per range statement, and floating-point
// addition is not associative, so face scores, the alphabet's spread and
// the naming of an anomaly's nearest character came out slightly different
// between runs. One label in five changed its alcohol verdict.
//
// This test fixes twenty labels, verifies them five times in this process
// and five times in child processes, and requires all ten digests to be
// equal. It is the property, not the claim: revert any of the three fixes
// and it fails.

const (
	determinismLabels = 20
	determinismSeed   = 9
	childEnv          = "TREASURY_DETERMINISM_CHILD"
)

// sampleLabels renders the fixed sample. The labels are generated here from
// a seed and the bundled faces, so the test carries its own inputs.
func sampleLabels(t testing.TB) ([]*image.Gray, []ttb.Expected) {
	t.Helper()
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	pool := ttb.NewFacePool(faces)
	if len(pool.Body) == 0 {
		t.Skip("no bundled body face")
	}
	rng := rand.New(rand.NewSource(determinismSeed))
	var imgs []*image.Gray
	var exps []ttb.Expected
	for len(imgs) < determinismLabels {
		doc, exp, _ := ttb.Generate(rng, pool, 0.2)
		img, _, err := synth.Render(doc, faces)
		if err != nil {
			continue
		}
		imgs = append(imgs, img)
		exps = append(exps, exp)
	}
	return imgs, exps
}

// digestOnce verifies the sample and returns a digest of every verdict and
// every piece of evidence.
//
// The stage timings step 26a added are cleared first, and the reason is
// the property itself: a wall-clock duration is not reproducible on any
// machine, so hashing one would make this test fail forever and say
// nothing. What the timings carry that IS deterministic - how many boxes
// were detected and how many runs were built from them - stays in the
// digest, so a change in what the reader found still moves it.
func digestOnce(t testing.TB) string {
	t.Helper()
	imgs, exps := sampleLabels(t)
	eng, err := verify.New(verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for i, img := range imgs {
		refs, claims := ttb.Inputs(exps[i])
		res, err := eng.Verify(context.Background(), img, refs, claims)
		if err != nil {
			t.Fatal(err)
		}
		res.Stages.Detect, res.Stages.Recognise = 0, 0
		res.Stages.TurnedPass, res.Stages.SecondPass = 0, 0
		res.Stages.Runs, res.Stages.Decide = 0, 0
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestMain lets the test binary act as its own child: with the environment
// variable set it prints one digest and exits, which is how the separate
// processes are run.
func TestMain(m *testing.M) {
	if os.Getenv(childEnv) != "" {
		fmt.Println(digestOnce(childTB{}))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// childTB satisfies the little of testing.TB that digestOnce uses when the
// binary runs as a child rather than as a test.
type childTB struct{ testing.TB }

func (childTB) Helper()                                         {}
func (childTB) Skip(...any)                                     { os.Exit(2) }
func (childTB) Fatal(args ...any)                               { fmt.Fprintln(os.Stderr, args...); os.Exit(1) }
func (childTB) Fatalf(f string, args ...any)                    { fmt.Fprintf(os.Stderr, f+"\n", args...); os.Exit(1) }
func (childTB) Errorf(f string, args ...any)                    { fmt.Fprintf(os.Stderr, f+"\n", args...) }
func (childTB) Logf(format string, args ...any)                 {}
func (childTB) Log(args ...any)                                 {}
func (childTB) Name() string                                    { return "child" }
func (childTB) Cleanup(func())                                  {}
func (childTB) Setenv(key, value string)                        {}
func (childTB) TempDir() string                                 { return os.TempDir() }
func (childTB) Failed() bool                                    { return false }
func (childTB) Error(args ...any)                               { fmt.Fprintln(os.Stderr, args...) }
func (childTB) Fail()                                           {}
func (childTB) FailNow()                                        { os.Exit(1) }
func (childTB) Skipped() bool                                   { return false }
func (childTB) SkipNow()                                        { os.Exit(2) }
func (childTB) Skipf(f string, args ...any)                     { os.Exit(2) }
func (childTB) Chdir(dir string)                                {}
func (childTB) Context() context.Context                        { return context.Background() }
func (childTB) Attr(key, value string)                          {}
func (childTB) Output() interface{ Write([]byte) (int, error) } { return os.Stdout }

// TestDeterminismSample verifies twenty labels five times in this process
// and five times in separate processes, and requires every digest to agree.
func TestDeterminismSample(t *testing.T) {
	if testing.Short() {
		t.Skip("long: twenty labels verified ten times")
	}
	var digests []string
	for range 5 {
		digests = append(digests, digestOnce(t))
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), childEnv+"=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("child %d: %v", i+1, err)
		}
		digests = append(digests, strings.TrimSpace(string(out)))
	}
	for i, d := range digests {
		if d != digests[0] {
			t.Errorf("digest %d of %d differs:\n  first %s\n  this  %s\n(runs 1-5 are in-process, 6-10 are separate processes)",
				i+1, len(digests), digests[0], d)
		}
	}
	t.Logf("%d labels, %d runs, digest %s", determinismLabels, len(digests), digests[0][:16])
}

// TestClaimSetIndependence pins what step 13a fixed: a claim's verdict must
// not depend on which other claims were in the run, nor on the order they
// were given in.
//
// TestDeterminismSample could not have caught this. It repeats one input,
// so every run asks the same questions in the same order and the shared
// code cache is filled the same way each time. The defect showed only when
// the input changed: the learned code of a component was cached per box at
// the first framing any claim asked for, so adding a second accepted
// spelling to one claim moved another claim's alcohol content from verified
// to not found on a real label.
//
// Order must not matter at all. Membership may, but only through the
// harvest, which is a designed coupling: a claim that decides teaches the
// alphabet the characters it printed, and a later claim reads them. So the
// subset arm drops only claims that decided nothing, which teach nothing,
// and requires every other verdict to be unchanged.
func TestClaimSetIndependence(t *testing.T) {
	if testing.Short() {
		t.Skip("long: one label verified nine ways")
	}
	imgs, exps := sampleLabels(t)
	eng, err := verify.New(verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	const labels = 4
	rng := rand.New(rand.NewSource(13))
	for i := 0; i < labels && i < len(imgs); i++ {
		refs, claims := ttb.Inputs(exps[i])
		full, err := eng.Verify(context.Background(), imgs[i], refs, claims)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{}
		decided := map[string]bool{}
		for _, v := range full.Claims {
			want[v.Claim] = verdictDigest(t, v)
			decided[v.Claim] = v.Status == verify.Verified || v.Status == verify.Mismatch
		}
		check := func(what string, got verify.Result) {
			for _, v := range got.Claims {
				if w, ok := want[v.Claim]; ok && w != verdictDigest(t, v) {
					t.Errorf("label %d, %s: claim %q differs from the full run\n  full %s\n  this %s",
						i, what, v.Claim, w, verdictDigest(t, v))
				}
			}
		}
		for p := range 3 {
			shuffled := append([]verify.Claim(nil), claims...)
			rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
			got, err := eng.Verify(context.Background(), imgs[i], refs, shuffled)
			if err != nil {
				t.Fatal(err)
			}
			check(fmt.Sprintf("permutation %d", p+1), got)
		}
		for _, c := range claims {
			if decided[c.Name] {
				continue // it taught the alphabet; dropping it may change the rest
			}
			subset := make([]verify.Claim, 0, len(claims)-1)
			for _, o := range claims {
				if o.Name != c.Name {
					subset = append(subset, o)
				}
			}
			got, err := eng.Verify(context.Background(), imgs[i], refs, subset)
			if err != nil {
				t.Fatal(err)
			}
			check("without "+c.Name, got)
		}
	}
}

// verdictDigest is a verdict and all its evidence as one string.
func verdictDigest(t testing.TB, v verify.Verdict) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
