# Approach

Live: https://label-verifier-173761965521.us-central1.run.app
Full measurement record, every table in the order it was measured: [docs/measurements/README.md](docs/measurements/README.md)

## The approach

An application states what a label must say. The engine reads the label, compares what it read to what was filed, and answers per claim with the evidence: the text it read, the spelling it compared that to, how far apart they were, and a crop of the part of the label the answer rests on. Where the evidence does not decide, it says so. `REVIEW` and `NOT_FOUND` are answers, not failures. The one thing this build is designed never to do is assert something about a label that the label does not bear out.

A label is scene text, not a document: words in whatever face the designer chose, at whatever size and angle, over artwork. A pretrained detector proposes the regions that hold text and a pretrained recogniser reads each region whole. Both are ONNX models embedded in the Go binary and run on the CPU, in one process, with no network call at run time.

What the reader returns is then judged by rules that were the design from the start: a claim is compared to runs of adjacent detections by a bounded edit distance; a radius decides whether the claim is present; a margin decides which value is present when several are close; a number is read from the label and validated against the values the regulation allows; a name taken from inside a longer reading must be the whole name, delimited by punctuation; a reading with characters missing may not contradict the application. Absence is reported as absence.

Each of those rules exists because a specific false assertion made it necessary, and the label that forced each one is named in the record. None was added on principle.

The statutory health warning (27 CFR 16.21) is verified on an exact match only, because no distance separates a damaged reading of the true statute from a clean reading of an altered one: the corpus's own alteration is four characters in 241, and the recogniser's damage on compliant labels is larger than that. Its header is checked for capitals from the text; heavier weight is measured from the pixels and, where the measure cannot decide, reported for review with the ratio as evidence rather than asserted either way.

## Tools used

- Go 1.24, one binary: engine, API, batch, and the operator's page.
- PP-OCRv4 detection (DBNet) and recognition (CRNN), Apache-2.0, as ONNX models embedded in the binary; ONNX Runtime 1.24 or later via `onnxruntime_go` (the binding asks for API version 24, and older runtimes refuse).
- Hand-written OpenAPI specification; server types generated from it with `oapi-codegen`, handlers by hand; CI fails if spec and code drift.
- A synthetic label generator whose parameters were measured from fifty real COLA registry labels (fonts, conventions, artwork, contrast, crowding), used for tuning with font families and labels held out.
- Distroless, non-root container on Cloud Run.

## Assumptions

- The statutory warning text is taken from 27 CFR 16.21 verbatim; alcohol content and net contents formats, standards of fill, and the phrases a statement of responsibility may begin with are taken from 27 CFR parts 4, 5, and 7, with citations in `ttb/`.
- A filed value and a printed value are the same claim across punctuation, spacing, diacritics, the whisky/whiskey spelling the regulation permits, and the permittee's two filed names. They are not the same across a dropped legal suffix, an abbreviation expansion, or a reordered name.
- Images are what an agent would upload: a scan or a reasonably frontal photograph. Rotated and inverted text is detected; heavy glare and steep angles degrade reading and the engine refuses rather than guesses.
- Nothing is stored. An uploaded image lives as long as the request that carried it.
- No external service is called. Their network blocks outbound traffic; the vendor pilot they described failed on exactly that, and this design depends on nothing outside the binary.

## Results, on fifty real labels from the public COLA registry

156 of 192 carried claims verified, precision 1.00, zero false assertions; the 122 claims those labels do not carry all reported as absent. Across the fifty and 500 synthetic labels, 2,572 verifications and not one wrong value named. Of the 36 unverified claims, 14 are refusals the engine is right to make (a filed brand's words inside a different company's name), and the rest are text the reader never returned or read with characters missing.

The statutory warning is verified exactly on 13 of the fifty, read but not exactly on 26, and not read at all on 11; its header is verified in capitals and heavier on 21, with the weight reported as uncertain on 17. No false assertion about a warning on any set.

Latency, measured on the fifty, each figure the median of three runs alone on the machine: locally 1.6 s median and 4.7 s p95 at two cores, 1.3 s and 3.9 s at four.

Deployed at four vCPU, the same fifty measured three times from a workstation over the public internet: **4.2 s median and 9.1 s at the 95th percentile end to end**, of which 3.2 s and 8.6 s are the engine's own time and the rest is the upload and the network; 6.3 to 7.1 s on the first request after an idle period; 156 verified in every run, none failed. A Cloud Run vCPU is roughly half a core of the machine the local figures were taken on, which is the whole of the difference — the wall clock sits within a second of what the service reports spending.

## What shipped

Single-label verification with evidence crops; batch verification, taking a CSV of claims and a ZIP of images and answering NDJSON one line per label as it finishes; the operator's page, with panels for one label and for many; `/health` with build identity and model hashes; the OpenAPI specification served by the binary; the warning check; and in-process hardening — body limits, a pixel cap read from the image header, a per-IP token bucket, a daily inference budget, timeouts that cancel into the engine, recovery, and the browser headers.

## Limits, stated

- Text the reader does not return cannot be verified. A stacked logotype brand, or back-label type at the recogniser's floor, comes back `NOT_FOUND`, not wrong.
- A brand name that appears only inside another company's name is refused, correctly, and the same rule refuses a few genuine brands set beside an ordinary word; no position or size feature separates those two cases on this population, and that was measured rather than assumed.
- The header-weight measure cannot separate bold from regular at warning type sizes on photographs; it reviews with the ratio shown. On the fifty, whose headers the regulation requires to be bold, the ratio runs from 0.69 to 1.79.
- The synthetic corpus reads its own warning on only 21 of 250 labels, because that warning is the smallest type it sets; so the corpus cannot test the warning check, and unit tests feed the readings directly instead.
- Rate limits and the daily budget are per instance, not per deployment, because they are held in process rather than in a shared store.
- The five-second p95 target is met locally at two cores and with room at four, and is **not** met on the deployed shape: 8.6 s of engine time at four vCPU. A Cloud Run vCPU is about half a core of the machine the local figures come from, so closing it needs a faster core or less work on the slowest label, not a deployment setting.

## This was not the original design

The engine built through the first eighteen steps read a label by cutting ink into glyphs and learning each label's alphabet from the statutory warning. Measured directly, that stage was correct on 0.39 of characters and on 0.25 through a camera channel, and everything downstream ran on those pieces. It was retired and replaced; every table from every step is kept in `docs/measurements/README.md`, in the order measured, including the numbers that were later corrected and why, because those tables are the evidence for the decision.
