package verify_test

import (
	"context"
	"testing"
	"time"

	"treasury/ttb"
	"treasury/verify"
)

// TestACancelledVerificationStops is the test the documents were owed:
// APPROACH.md has said since step 30's fifth item that timeouts cancel
// into the engine, and until now Verify took a context and never read it,
// so a caller who hung up held the only instance to completion.
//
// Three things are checked, because "it returns an error" is the least
// interesting of them.
func TestACancelledVerificationStops(t *testing.T) {
	if testing.Short() {
		t.Skip("long: loads the reader and verifies a label")
	}
	imgs, exps := sampleLabels(t)
	if len(imgs) == 0 {
		t.Skip("no bundled body face")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	refs, claims := ttb.Inputs(exps[0])

	// Already cancelled: nothing should be read at all.
	t.Run("a context cancelled before the call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		res, err := eng.Verify(ctx, imgs[0], refs, claims)
		if err == nil {
			t.Fatal("a cancelled context returned no error")
		}
		if len(res.Claims) != 0 || len(res.Regions) != 0 {
			t.Errorf("a cancelled verification returned a result: %d claims, %d regions",
				len(res.Claims), len(res.Regions))
		}
		// It should give up at the first check, not after a detection.
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("took %s to give up on an already-cancelled context", d)
		}
	})

	// Cancelled while it works: the point is that it stops early, which a
	// test can only show by comparing against how long the whole thing
	// takes. So the whole thing is timed first.
	whole := func() time.Duration {
		start := time.Now()
		if _, err := eng.Verify(context.Background(), imgs[0], refs, claims); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}()

	t.Run("a deadline that expires part way", func(t *testing.T) {
		// Long enough to be inside the work, short enough that finishing
		// would take conspicuously longer. A crop cannot be interrupted,
		// so some overshoot is expected and the bound is generous.
		ctx, cancel := context.WithTimeout(context.Background(), whole/4)
		defer cancel()
		start := time.Now()
		res, err := eng.Verify(ctx, imgs[0], refs, claims)
		took := time.Since(start)
		if err == nil {
			t.Skipf("the label verified inside %s, so nothing was cancelled", whole/4)
		}
		if len(res.Claims) != 0 {
			t.Errorf("a timed-out verification returned %d claims", len(res.Claims))
		}
		if took >= whole {
			t.Errorf("cancelled at %s but ran for %s, and the whole verification takes %s: "+
				"the context is not being honoured", whole/4, took, whole)
		}
	})

	// The pool: whatever the cancellations did, the engine must still
	// work. A session left borrowed would show up here as a hang, since
	// the next request would wait for one that never comes back.
	t.Run("the engine still works afterwards", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if _, err := eng.Verify(context.Background(), imgs[0], refs, claims); err != nil {
				t.Error(err)
			}
		}()
		select {
		case <-done:
		case <-time.After(2*whole + 30*time.Second):
			t.Fatal("a verification after two cancellations did not finish: " +
				"a reader session did not come back to the pool")
		}
	})
}
