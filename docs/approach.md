# Approach

## The problem as decoding

An application states what a label must say. The label image is that message received over a noisy channel: an unknown typeface at unknown sizes, lighting, angle, blur, and compression. Verification asks, for each expected value, whether the image contains a region whose code lies within a Hamming ball around a codeword for that value. Text is never the unit; bit codes are.

The codebook does not come from a font library. It comes from the image. Every compliant label carries the statutory government warning, about sixty words of known, verbatim text in the label's own type. Aligning that block to the known string teaches the engine the label's alphabet: this shape is an "e" in this face, weight, and lighting. Every other field is read with that alphabet. Only glyphs the warning lacks, digits and most capitals, are synthesized, in the bundled faces nearest the learned letterforms, and even those are replaced by the label's own glyphs once a claim that contains them has verified.

Nothing in the core knows what a label is. The engine takes a reference (text known to be printed, with spans expected in a heavier weight) and claims (expected text, or an enumeration of the values a field may take). The TTB case is a thin configuration: the reference is 27 CFR 16.21, the emphasis span is "GOVERNMENT WARNING:", the claims are brand, class, producer, origin, alcohol content, and net contents. A random photo of anything containing a known string goes through the same path.

## Pipeline

1. **Preprocess.** Grayscale, longer side to 1600 px, Sauvola threshold (window 31, k 0.2), deskew by projection-profile sweep, glare mask when the brightest percentile stands clear of the paper.
2. **Regions.** Connected components with dots folded into the glyph beneath; lines by vertical centre; regions are lines, bands, and every run of up to five words, so a value inside a sentence has a region of its own.
3. **Alphabet.** The densest vertically adjacent block is aligned to the reference by dynamic programming over (glyph, character) with match, merge (two or three characters printed touching), split (two or three components for one character), insert, delete, and space steps. The first pass uses shape-class priors derived from the characters (tall, descending, small, width); later passes use the distance to each character's centroid. The band is five percent of the glyph count and widens when the path touches its edge. Output: glyph samples per character, header samples apart, centroids, within-character spread, per-row quality.
4. **Codewords.** A claim's text is spelled by placing the medoid sample of each character on a baseline at the label's own letter and word gaps; characters the reference lacks are synthesized in the bundled face nearest the learned letterforms, their strokes brought to the learned stroke width, with the next-nearest faces as alternatives. Free text is spelled in its casing, quote, and weight variants. Enumerations spell every value in every printed form.
5. **Decode.** Every structurally plausible (region, candidate) pair is scored by aligning the region's components to the candidate's glyph codes with the same DP, so a single differing digit counts. The nearest region reads as its nearest candidate; the competitor is the nearest candidate of a different value on the same region under the same geometry. A claim is decided when the decisive readings among the regions near the best agree.
6. **Verdicts.** VERIFIED, MISMATCH (observed value named), REVIEW (candidates named), NOT_FOUND, SKIPPED. Reference rows verify, review, or fail on their own alignment anomalies; the emphasis span is heavy or regular by comparing it to itself at the body's stroke width and at 1.25 times it. Every verdict carries the region, distances, radius, what was synthesized, and the deskew angle.

## The glyph code

The spec's line hash (64×16 coverage plus a difference hash) is kept for regions without components and as evidence, but it does not decide anything. Measured against real lines it has a phase-noise floor near fifteen percent of its bits, larger than one differing digit, and as a ranker it put dozens of short spurious pairs ahead of the true one whenever the label's face differed from the synthesized one.

The code that decides is per glyph: a positional view of the glyph inside a frame of 2.2 x-heights (16×16 cells, so size and position above the baseline are part of the code) concatenated with a tight view of the ink's own box (12×16), both with coverage smoothed over neighbouring cells and quantized to four levels. The configuration was chosen by measurement (`internal/spell/frame_test.go`): across sizes the same character drifts by under a third of the distance between two different characters, the closest genuine pair stays 1.4 times the worst drift apart, and a four percent x-height or one pixel baseline error costs a fifth of a pair distance. Sharper grids and single views fail those tests.

This code is the error-correcting code of the channel in the sense the spec asks for: the alphabet's centroids are the codewords, the within-character spread is the noise, and the tie margin is set from that spread.

## Evaluation protocol

The synthetic set is drawn with faces the engine does not bundle. Labels drawn in the bundled faces would match synthesized digits by the same face that synthesized them; the evaluation refuses such a set unless told otherwise, and then stamps its table. The reported set uses the machine's installed faces: 26 families with both a regular and a bold, 117 text-capable faces for brand lines, including handwriting and decorative faces. Twenty percent of labels carry a deliberate error (wrong brand, wrong class, wrong alcohol content, wrong net contents, title-case header, regular-weight header, altered warning wording, a missing claim); eighty percent are pushed through the channel (rotation ±10°, perspective, blur σ ≤ 1.5, JPEG 40–95, brightness and contrast, glare). Thresholds are chosen on the even-numbered labels and reported on the odd-numbered ones.

### Results: 500 held-out labels, 2026-09-03

Generated with `gen set -n 500 -seed 1 -fontdir C:\Windows\Fonts` (26 body families with regular and bold, 117 text-capable faces; none bundled), evaluated with `eval -set synth -workers 10 -tune`. Latency is per label with ten labels in flight on one desktop CPU; a single verification takes about half that. Precision and recall are of VERIFIED against what was printed; "mismatch found" counts labels printed with a wrong value that were called MISMATCH; "not found on missing" counts claims left off the label that were called NOT_FOUND.

#### Whole set, engine defaults

500 labels, 135 without an alphabet, latency median 16.3s p95 24.3s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 278 | 0.97 | 0.69 | 0.00 | 0/5 | 0 |
| class | 500 | 1.00 | 0.69 | 0.00 | 0/10 | 11 |
| producer_1 | 500 | 1.00 | 0.70 | 0.00 | 0/0 | 0 |
| producer_2 | 500 | 1.00 | 0.70 | 0.00 | 0/0 | 0 |
| origin | 500 | 1.00 | 0.69 | 0.00 | 0/0 | 0 |
| abv | 500 | 1.00 | 0.27 | 0.36 | 3/12 | 1 |
| net | 500 | 1.00 | 0.49 | 0.21 | 5/5 | 1 |
| brand (display face) | 222 | 0.99 | 0.66 | 0.00 | | |

Reference rows: compliant labels with every row verified 155/407 (194 reviewed, 58 failed); wording and title-case errors caught 12/23.
Emphasis: correct on 208/296 labels (compliant headers verified and regular-weight headers caught).

#### Tuning on half A (250 labels): mean F1 0.755

#### Half B under the values chosen on half A ( free radius 0.12, enum radius 0.15, tie 0.010)

250 labels, 67 without an alphabet, latency median 15.6s p95 24.5s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 137 | 0.99 | 0.69 | 0.00 | 0/1 | 0 |
| class | 250 | 1.00 | 0.71 | 0.00 | 0/5 | 1 |
| producer_1 | 250 | 1.00 | 0.70 | 0.00 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.71 | 0.00 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.69 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.33 | 0.22 | 3/8 | 1 |
| net | 250 | 0.99 | 0.59 | 0.10 | 3/3 | 0 |
| brand (display face) | 113 | 0.99 | 0.67 | 0.00 | | |

Reference rows: compliant labels with every row verified 80/208 (100 reviewed, 28 failed); wording and title-case errors caught 4/9.
Emphasis: correct on 113/155 labels (compliant headers verified and regular-weight headers caught).

Reference row fail threshold (anomaly weight), half A → half B:

| threshold | errors caught (A) | compliant false fails (A) | errors caught (B) | compliant false fails (B) |
|---|---|---|---|---|
| 1.0 | 11/14 | 53/137 | 5/9 | 49/151 |
| 1.5 | 10/14 | 35/137 | 4/9 | 33/151 |
| 2.0 | 8/14 | 30/137 | 4/9 | 28/151 |
| 3.0 | 7/14 | 23/137 | 3/9 | 23/151 |
| 4.0 | 7/14 | 20/137 | 3/9 | 20/151 |


The tuned values (free-text radius 0.12, enumeration radius 0.15, tie 0.01) are the engine's defaults.

### Alphabet robustness, half B re-run, 2026-09-03

The dominant loss in the table above was labels that learned no alphabet: 27 percent, and every claim on them is NOT_FOUND. Tracing those labels found three causes, none of them the alignment itself. Blur fuses letters into words and the splitter measured its yardstick from the fused line, so it never cut. Light-gray text at gray 160 to 207 on a 245 background loses half its strokes to the threshold and shatters into fragments. And at x-heights of 10 to 14 pixels the shape-class tests sat on pixel boundaries: a 3 px descender is exactly a quarter x-height, a period is four to nine pixels of area against a floor of eight, and the alignment absorbed the dropped period into its neighbour as a merge that counted as a violation.

Three mechanisms answer them. Components are cut at necks, columns where the thickest ink is under three quarters of the stroke, and a component wide enough for three glyphs is also cut at single-run minima of its profile; the alignment's split path re-joins over-cuts and its merge path covers fusions the cutter misses. Thresholding is hysteretic: a permissive Sauvola pass is taken whole only for components where the strict pass found just the cores. Shape classes have dead bands, the width miss is uncapped, and the violation rate is per explained character. A wrong reference of the same length still scores 0.15 to 0.35 on that rate against 0.03 to 0.11 for real blocks.

Same 250 labels of half B, same protocol:

250 labels, 16 without an alphabet, latency median 18.5s p95 29.9s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 137 | 0.99 | 0.79 | 0.01 | 0/1 | 0 |
| class | 250 | 1.00 | 0.72 | 0.00 | 0/5 | 1 |
| producer_1 | 250 | 1.00 | 0.75 | 0.00 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.76 | 0.00 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.72 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.31 | 0.41 | 1/8 | 1 |
| net | 250 | 0.99 | 0.53 | 0.25 | 2/3 | 1 |
| brand (display face) | 113 | 0.98 | 0.74 | 0.00 | | |

Reference rows: compliant labels with every row verified 82/208 (75 reviewed, 51 failed); wording and title-case errors caught 6/9.
Emphasis: correct on 144/198 labels (compliant headers verified and regular-weight headers caught).

Labels without an alphabet fell from 70 to 16 (6.4 percent). Among the 180 labels that already learned one, free-text recall is unchanged (0.915 against 0.92); the 54 newly recovered labels verify 30 percent of their free text, because their claim lines are as fused or as faint as their warnings were. Per-character acceptance produced two `char_unlearned` reviews in 250 labels. The alphabet gates on the sample hold at 99.2, 99.2, and 99.3 percent.

### Digits: a classifier, not an encoder (amendment step 2, 2026-09-03)

The reference never contains digits, so the engine carried none of their shapes and read them through glyphs synthesized in a bundled face. That is a ten-class prior-knowledge problem, and it is now a small convolutional network: three blocks of two 3×3 convolutions (16, 32, 48 channels), 51,902 parameters, fourteen classes (0–9, %, period, comma, other). The training data comes from the engine's own channel: `cmd/digits gen` renders the classes and other characters in every installed face and the bundled ones at x-heights 10, 14, and 20, runs the label generator's augmentation and the engine's preprocessing (resize, hysteresis threshold, deskew), and cuts gray frames at the truth positions with the geometry jittered as the engine will estimate it. A frame is 1.6 by 2.2 x-heights on the row's baseline, ink bright, scaled by the patch's own contrast. Families are held out by a hash of their name: 57 of 226. Batch norm is folded at export; the weights are embedded and run in plain Go (3 ms a frame, no cgo), with the same network behind the `onnx` build tag through onnxruntime for parity, which holds to 4e-6 in the logits and to the frame in held-out counts.

Two findings from the first runs. The frame normalization took its ink level from the 5th percentile of the patch, which for a period or a comma, under two percent of the patch, lands in the background; the third darkest cell is the ink now. And letters shaped like digits in many faces (O, l, I, S, B, Z, g, q, b, D) were training the other class and capping digit recall at 96 percent on unseen faces; the classifier says which digit a glyph is, and whether it is a digit at all is the engine's call from context, so those letters left the set. Half the training frames are condensed, extended, or slanted to stand in for faces the installed set covers thinly.

Held-out digit accuracy after three runs: 96.3, 96.1, 96.7 percent, against the amendment's gate of 98. The gate is not met. Thirty-four of the 57 held-out families are at or above 98 percent and the tail is display type: Gill Sans Extra Condensed Bold 65, Palace Script 69, Magneto 75, Rage Italic 78, Freestyle Script 80, Vermin Vibes 89, Bauhaus 93 90, Broadway 92, Wide Latin 93, Ravie 93. The remaining confusions are 1 read as 7 (137 of 3,586), comma as period and period as comma (319 of 10,444), and 7 as other (63). The pure-Go forward pass and onnxruntime give the same 48,859 of 50,546 and agree in every logit to 4e-6. A label's alcohol and net fields are set in text faces, where the classifier is at or above the gate; the number reported is the honest one on the split as drawn.

### Numeric fields are read, not enumerated (amendment step 3, 2026-09-03)

Alcohol content and net contents were decoded against enumerations, 1,520 and 100 candidate texts, and the enumeration did the reading. Now a numeric claim carries its printed formats with a placeholder for the number and a scale into the claim's unit (proof is half a percent, a litre a thousand millilitres), a set of valid values, and a tolerance. The engine classifies the components of every region on a line short enough to be a field, skipping lowercase-height components, and takes as the reading the longest run of digit classes that stands as a word: bounded by word gaps, at most an opening paren before it and an attached unit after it. Each format instantiated with the reading, and with the runner-up class of the least certain digit, becomes a candidate, and the candidates are decided exactly as free text is: aligned glyph by glyph against the region with the alphabet's letters for the unit, the same radius, tie, and peer rules, the same evidence. The decided value is then judged in the claim's unit: within tolerance of the claim VERIFIED, a valid other value MISMATCH naming it, a value the field cannot take REVIEW. The valid set is a validity check on a reading, not a codebook. Two rules changed on the way: a competitor beyond the radius is not a competitor, since a candidate of another format differs in most of its glyphs and demanded a margin no gap could meet; and a three-way merge counts for the two extra characters it absorbs, since two of them fitted "fl. oz." over "mL" for half a glyph.

Tracing the first run's alcohol misses found the readings wrong or absent rather than the alignments far. A percent sign is often three components, two circles about a slash, and the classifier knows the slash; the circles are absorbed by it. A glyph too small to classify between two digits is a decimal point or a thousands comma by where it sits. And line grouping used the global median glyph height and width, which a label's smallest type sets, for its centre and gap tolerances, so on a line of tall type every period became a line of its own and every space split the line; a band's own median now sets them. The radius for numeric claims, re-tuned on half A with the classifier reading the digits, stays at 0.15.

Half B, single-threaded, the same 250 labels as above:

250 labels, 15 without an alphabet, latency median 2.7s p95 4.1s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 137 | 0.99 | 0.79 | 0.01 | 0/1 | 0 |
| class | 250 | 1.00 | 0.73 | 0.00 | 0/5 | 1 |
| producer_1 | 250 | 1.00 | 0.75 | 0.00 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.76 | 0.00 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.72 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.59 | 0.13 | 3/8 | 3 |
| net | 250 | 1.00 | 0.56 | 0.22 | 2/3 | 2 |
| brand (display face) | 113 | 0.98 | 0.77 | 0.00 | | |

Reference rows: compliant labels with every row verified 83/208 (73 reviewed, 52 failed); wording and title-case errors caught 6/9.
Emphasis: correct on 145/199 labels (compliant headers verified and regular-weight headers caught).

The gate was latency: median 2.7 s and p95 4.1 s against 5.0 and 7.0 (it was 8 to 10 s a label single-threaded before this step). Alcohol content went from recall 0.31 to 0.59 at precision 1.00, with review down from 0.41 to 0.13 and three of eight wrong values named; net contents from 0.53 to 0.56. Of the alcohol misses that remain, 52 are NOT_FOUND: on 15 no alphabet was learned, and on the rest the line was read but its unit, set in capitals the reference never teaches and synthesized in a foreign face, aligned outside the radius. Net's reviews are mostly `too_few_glyphs`: "1 L" is two glyphs and cannot decide anything on its own.


### Real labels: ten from the COLA registry (amendment step 4, 2026-09-03)

Ten approvals completed 20 to 26 August 2026 were taken from the TTB public COLA registry, four spirits, three wines, three malt beverages, each from a different permittee, with the brand, class, applicant, and origin from the application form and the alcohol content and net contents transcribed from the label images, which the form does not carry. Front, back, and neck images were stacked into one image (`cmd/stack`); the set is `real/`, with the TTB IDs and source URLs in each truth file. The images are the artwork as submitted, 96 to 300 dpi, clean: no blur, no glare, no compression to speak of.

**Step 4 as specified: the engine at the step 3 commit, unchanged, on the ten labels** (`real/table_step4_baseline.md`, `real/records_step4_baseline.json`):

10 labels, 9 without an alphabet, latency median 0.3s p95 4.6s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 10 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| class | 10 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| producer_1 | 10 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| producer_2 | 6 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| origin | 3 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| abv | 10 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| net | 10 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 0/10 (9 reviewed, 1 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 1/1 labels (compliant headers verified and regular-weight headers caught).

Nine of ten learn no alphabet; the tenth, Seelbach's, learns one from its letterspaced warning and verifies nothing, every claim aligning at 18 to 20 percent against radii of 12 and 15. Read against the labels, the causes are these, and none of them is in the synthetic channel:

1. **The reference is set in capitals on half of real labels** (five of ten). The generator never did that, and a mixed-case reference aligned to capitals is rejected every time.
2. **Type is set light on dark, and the warning runs vertically** (two labels each). The generator never did either.
3. **The warning sits inside a taller block of similar rows**: ingredients, importer, and producer lines in the same size directly above or below it (0003, 0010). The generator isolated the warning with leading.
4. **The claims are in other faces.** On synthetic labels the class, producer, and origin lines were set in the warning's face and matched at 3 to 8 percent. On real labels only the warning is in the warning's face; the claims are in two to four others.
5. **Numbers are printed in forms the list lacked**: "ALC 19% by Vol.", "20% Alc by Vol", "ALC. BY VOL. 5%", "60 % ALC/VOL", and malt-beverage fills and strengths outside the wine and spirits sets (355 mL, 3.75%, 4.1%).

**The synthetic channel is gentler than reality in every way that matters and harsher in the one that does not.** Harsher: it blurs, rotates, warps, and compresses, and registry artwork has none of that; every real label thresholds cleanly. Gentler: it set the warning in mixed case, dark on light, horizontal, alone, and set the claims in the warning's face.

### Step 4b: mechanisms for what the real labels showed (2026-09-03)

This step was not in the amendment. It changed the engine while step 4 was being measured, and its reason is recorded here after the fact, against the amendment's rule; the step 4 table above was re-measured afterwards from the step 3 commit so that both numbers stand. Its gate, also stated after the fact: more of the ten learn an alphabet than the one above, and the synthetic half B does not regress beyond noise.

The mechanisms, each general: the reference is also tried in capitals, with the block's x-height estimate divided by the cap-to-x ratio since an all-capitals block has no x-height to measure, and which casing the label printed is read off the block's glyph heights (capitals stand uniformly tall; matched counts and violation fractions both preferred capitals on mixed-case blocks, because with the scaled x-height every glyph measures tall and contradicts nothing). The image is tried inverted and in the other three orientations when no alphabet is learned, and no further. Clusters larger than the reference are searched by windows of consecutive rows. A numeric claim is decided only on regions holding a digit run. The regulation's alcohol-statement forms and the malt-beverage fills, with a tenth-percent step for beer, are in the `ttb` data.

10 labels, 4 without an alphabet, latency median 3.6s p95 5.6s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 10 | 1.00 | 0.10 | 0.10 | 0/0 | 0 |
| class | 10 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| producer_1 | 10 | 1.00 | 0.10 | 0.10 | 0/0 | 0 |
| producer_2 | 6 | 0.00 | 0.00 | 0.17 | 0/0 | 0 |
| origin | 3 | 1.00 | 0.33 | 0.00 | 0/0 | 0 |
| abv | 10 | 0.00 | 0.00 | 0.20 | 0/0 | 0 |
| net | 10 | 1.00 | 0.10 | 0.00 | 0/0 | 0 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 1/10 (4 reviewed, 5 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 5/6 labels (compliant headers verified and regular-weight headers caught).

Per label, step 4 against 4b (V verified, M mismatch, R review, dash not found, s skipped):

| label | TTB ID | step 4 matched | step 4 claims | 4b taken, casing | 4b matched | 4b claims | what the label is like |
|---|---|---|---|---|---|---|---|
| 0001 | 26027001000487 | —/— | no alphabet | as_is, — | 23/150 | no alphabet | light type on black; warning in condensed caps |
| 0002 | 26044001000617 | —/— | no alphabet | rot90, upper | 49/264 | brand — class — produ — abv — net — | warning vertical; brand in script; class in display face |
| 0003 | 26051001000586 | 60/327 | no alphabet | as_is, — | 52/224 | no alphabet | clean; class and warning large |
| 0004 | 26166001000601 | 218/243 | brand — class — produ — abv — net — | as_is, as_given | 218/243 | brand — class — produ — abv — net — | warning letterspaced with items on separate lines; producer magenta on dark band |
| 0005 | 26054001000022 | 30/160 | no alphabet | as_is, upper | 196/233 | brand V class — produ V abv R net — | warning condensed caps; label prints "WITHE WINE" |
| 0006 | 26146001000213 | 28/145 | no alphabet | as_is, — | 95/236 | no alphabet | warning condensed caps on back; "ALC 19% by Vol." |
| 0007 | 26188001000248 | —/— | no alphabet | inverted, upper | 122/231 | brand R class — produ R abv R net V | single strip label, warning boxed |
| 0008 | 26033001000372 | 67/172 | no alphabet | as_is, — | 67/172 | no alphabet | can wrap; warning condensed caps in a column; beer fill 355 mL |
| 0009 | 26202001000918 | —/— | no alphabet | inverted_rot90, upper | 84/247 | brand — class — produ — abv s net — | light type on dark blue; warning vertical and tiny |
| 0010 | 25132001000733 | 73/433 | no alphabet | as_is, as_given | 183/245 | brand — class — produ — abv s net M | gold gradient; warning centered with items on their own lines; "ALC. BY VOL. 5%" |

Six of ten learn an alphabet against one. The step 4 table says something the augmented synthetic table could not: the synthetic channel was not wrong about noise, it was wrong about conventions. Capital warnings, light type on dark, vertical text, and claims in other faces are choices printers make, not degradations, and no amount of blur, rotation, or compression in the generator stood in for them. The two labels whose claims are in the warning's face verify them: EDDA's brand, producer, and origin at 5 to 10 percent, while it refuses the class line, which the printer set as "WITHE WINE" (`docs/evidence/edda_back.png`); THE AUSTIN WINERY's net contents, with its brand and producer at 8 to 9 percent held at review because the block's S samples disagreed (the per-character acceptance from step 1). Everything set in another face still aligns at 17 to 21 percent, right or wrong: the 0.12 and 0.15 radii tuned on the synthetic set sit at the noise floor of cross-face alignment, where a wrong "1 Litre" on 0010 lands at 15 percent next to right answers at 13 to 17. 0001 (reversed condensed capitals on black), 0003 (a distressed face that breaks glyphs), 0006, and 0008 (condensed capitals, 0008 in a narrow column) still learn nothing.

Synthetic half B, re-run with these mechanisms: alcohol 0.59, net 0.55, brand 0.79, 17 of 250 without an alphabet against 15 before, median 2.8 s and p95 3.9 s single-threaded. The gate holds.

Evidence crops in `docs/evidence/`: `edda_warning_caps.png` (the capitals warning aligned, matched glyphs in green), `edda_back.png` ("WITHE WINE"), `gaul_warning_vertical.png` (reversed, vertical, 5 px type), `seelbachs_warning.png` and `seelbachs_class.png` (the warning's letterspaced sans against the class line's bold serif), `boojies_block.png` (a clean large warning under two rows of the same size that the alignment still does not isolate).

What this changes about the plan: step 5's contrastive encoder was conditioned on free-text recall on augmented synthetic labels, and that recall is 0.72 to 0.79 with the misses mostly blur. The real labels say the limiting factor is the face: an alphabet learned from one face does not carry to a claim in another, and the bundled faces do not stand in. That is what a contrastive encoder trained on cross-face pairs would learn to ignore, and the current generator, which sets claims in the body face, would teach it nothing about it. The sequence is therefore: the generator sets claims in faces other than the body face, the table is re-run to measure the gap, and the encoder is trained on that data.

### Step 5a: the generator learns the real conventions (2026-09-03)

Gate, stated before the run: regenerate the 500 labels with claims set in faces other than the warning's and the conventions the real labels showed, at their real frequencies; re-run half B single-threaded and the ten real labels with the engine unchanged from step 4b; report the cross-face gap and the convention coverage with both numbers. No recall threshold, since 5a measures the gap 5b must close.

The generator now sets each of class, producer, origin, alcohol, and net in a body face of another family with probability 0.75; the warning in capitals with probability 0.5; the label light on dark 0.2; the warning vertical along the left side 0.2; rows of other text in the warning's size against it 0.2. On half B the set carries them at 47%, 16%, 21%, and 22% of labels, and 76% of claims are in another face. The real ten had 5, 2, 2, and 2 of 10 and every claim in another face.

Half B before, claims in the warning's face, no conventions (step 4b's engine on the old set): brand 0.79, class 0.73, producer 0.76, origin 0.72, alcohol 0.59, net 0.55, 17 of 250 without an alphabet, median 2.8 s. Half B after, same engine, the new set:

250 labels, 15 without an alphabet, latency median 3.1s p95 4.5s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 142 | 0.98 | 0.59 | 0.01 | 0/4 | 0 |
| class | 250 | 1.00 | 0.40 | 0.00 | 0/3 | 3 |
| producer_1 | 250 | 1.00 | 0.39 | 0.01 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.44 | 0.01 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.41 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.41 | 0.06 | 1/5 | 7 |
| net | 250 | 1.00 | 0.38 | 0.16 | 2/2 | 3 |
| brand (display face) | 108 | 0.95 | 0.51 | 0.00 | | |

Reference rows: compliant labels with every row verified 71/201 (66 reviewed, 64 failed); wording and title-case errors caught 4/10.
Emphasis: correct on 141/194 labels (compliant headers verified and regular-weight headers caught).

Cross-face gap (correct claims only; same-face = set in the warning's face):

| claim | same-face n | same-face recall | cross-face n | cross-face recall |
|---|---|---|---|---|
| class | 67 | 0.51 | 177 | 0.36 |
| producer_1 | 56 | 0.48 | 194 | 0.37 |
| producer_2 | 56 | 0.50 | 194 | 0.42 |
| origin | 68 | 0.53 | 182 | 0.37 |
| abv | 49 | 0.45 | 189 | 0.40 |
| net | 58 | 0.43 | 187 | 0.36 |

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | no alphabet | free-text recall |
|---|---|---|---|
| warning in capitals | 118 | 7 | 0.48 |
| light on dark | 40 | 1 | 0.41 |
| vertical warning | 52 | 1 | 0.00 |
| crowded warning | 56 | 6 | 0.43 |
| none of these | 68 | 4 | 0.53 |

The ten real labels, same engine: unchanged from step 4b, six of ten with an alphabet, brand, producer, and origin verified on EDDA and net on THE AUSTIN WINERY.

What the two tables say. The cross-face gap on synthetic labels is about a tenth: 0.43 to 0.53 same-face against 0.36 to 0.42 cross-face. It is smaller than the real labels' gap, which is everything, because the generator's other faces are regular text faces of other families and the nearest bundled face stands in for those tolerably, while real claims are set in bold, condensed, serif, and display faces the bundled set does not cover. The conventions cost more than the faces: labels with a vertical warning verify no free text at all, and that is a mechanism gap, not a decoding one. The engine learns the alphabet in the rotated image and then searches for claims in that same image, where every other line of the label is now vertical. Capitals cost five points (0.48 against 0.53 without any convention): the alphabet learned is capitals only, and lowercase claims are then synthesized. Light on dark costs twelve. Crowding costs ten and six of its 56 labels their alphabet. The alcohol and net readings fell from 0.59 and 0.55 to 0.41 and 0.38, since their units are now synthesized as often as any other claim.

Step 5b therefore has two parts to measure against these numbers: claims searched in the image's original orientation when the alphabet was learned in a rotated one, and the contrastive encoder for the cross-face distance.

### Step 5b: the contrastive encoder on cross-face pairs (2026-09-04)

Gate, stated before the run: on the new half B and the ten real labels, single-threaded, cross-face recall against 5a's, precision held at 1.00 on every claim, median under 5.0 s and p95 under 7.0 s, radii re-tuned on half A for the learned code first.

The encoder. `cmd/glyphs gen` renders every claim character (73 classes: letters, digits, and the punctuation claims use) in every installed and bundled face at three x-heights, through the label augmentation and the engine's preprocessing, and cuts binary frames at the alphabet's own geometry with the engine's framing jitter; 146,088 training frames from 169 families, 54,677 held-out frames from 57. A four-block network (12, 24, 40, 64 channels, one convolution in the first block and two in the others, 108,004 parameters) ends in a linear layer to 256 and tanh; the loss is supervised contrastive over the batch, with the same character in any face as a positive, plus a quantization term; the code is the sign bits. On held-out families the nearest character by Hamming distance to training prototypes is right 85 percent of the time in Python and 82 in Go, where the frames go through the engine's binary path; the dual hash scores 44 percent on the same frames. Same-character distances sit at a median of 0.04 and the nearest other character at 0.22. The first version, twice the cost, scored 86 and 83 and ran a label in 15 s.

How it is used. The alphabet is still learned with the hash, whose gates were measured with it; claims are decoded in the learned code, both the observed frames and the target codes, with the alphabet's samples recoded from their crops and synthesized glyphs encoded in it. A face-invariant code cannot rank faces, so the nearest face is still chosen in the hash. Since the encoder was trained under the engine's framing jitter, each component and each union box is encoded once and the code memoized, where the hash is recomputed at every scale and baseline the decoder tries; recoded samples and synthesized codes are cached across the two passes, composed runs across candidates, unions are only formed for gaps under 0.15 x-heights, and no three-way composed codes are made. The tuner on half A keeps the radii at 0.12 and 0.15 and moves the tie margin from 0.010 to 0.020.

Two engine changes measured by the same gate. Claims are now searched in the image's original orientation as well when the alphabet was learned in a rotated one (5a's vertical-warning labels verified nothing for this reason), and a numeric claim is decided per reading, on the regions overlapping that reading's region: with a face-tolerant code, "6% alc/vol" from an age line had matched a "41% alc/vol" line within the radius. Thin components many times wider than tall are dropped as rules: a boxed warning's edges had bridged a two-column label's rows into single lines. The inversion attempts moved to the end of the orientation ladder, so only a label that fails upright and rotated pays for them.

Half B, single-threaded, claims decoded with the learned encoder:

250 labels, 15 without an alphabet, latency median 3.6s p95 5.2s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 142 | 0.98 | 0.82 | 0.05 | 0/4 | 0 |
| class | 250 | 1.00 | 0.77 | 0.02 | 0/3 | 3 |
| producer_1 | 250 | 1.00 | 0.70 | 0.03 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.71 | 0.02 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.65 | 0.02 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.55 | 0.08 | 2/5 | 7 |
| net | 250 | 1.00 | 0.56 | 0.20 | 2/2 | 1 |
| brand (display face) | 108 | 0.96 | 0.85 | 0.01 | | |

Reference rows: compliant labels with every row verified 71/201 (67 reviewed, 63 failed); wording and title-case errors caught 4/10.
Emphasis: correct on 141/194 labels (compliant headers verified and regular-weight headers caught).

Cross-face gap (correct claims only; same-face = set in the warning's face):

| claim | same-face n | same-face recall | cross-face n | cross-face recall |
|---|---|---|---|---|
| class | 67 | 0.79 | 177 | 0.77 |
| producer_1 | 56 | 0.64 | 194 | 0.71 |
| producer_2 | 56 | 0.68 | 194 | 0.72 |
| origin | 68 | 0.69 | 182 | 0.63 |
| abv | 49 | 0.57 | 189 | 0.55 |
| net | 58 | 0.50 | 187 | 0.58 |

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | no alphabet | free-text recall |
|---|---|---|---|
| warning in capitals | 118 | 7 | 0.77 |
| light on dark | 40 | 1 | 0.59 |
| vertical warning | 52 | 1 | 0.63 |
| crowded warning | 56 | 6 | 0.67 |
| none of these | 68 | 4 | 0.76 |

The cross-face gap is closed: class 0.79 same-face against 0.77 cross-face (5a: 0.51 against 0.36), producer 0.64/0.68 against 0.71/0.72 (0.48/0.50 against 0.37/0.42), origin 0.69 against 0.63 (0.53 against 0.37), alcohol 0.57 against 0.55 (0.45 against 0.40), net 0.50 against 0.58 (0.43 against 0.36). Same-face recall rose with it, since the learned code also tolerates the channel better than the hash. Precision is 1.00 on every claim but brand, at 0.98 as in 5a, the same labels whose producer line names the brand. Latency: median 3.6 s, p95 5.2 s against 5.0 and 7.0; 15 of 250 without an alphabet. By convention: capitals 0.77 (5a: 0.48), light on dark 0.59 (0.41), vertical 0.63 (0.00, the upright search), crowded 0.67 (0.43), none 0.76 (0.53).

The ten real labels, same engine:

10 labels, 3 without an alphabet, latency median 5.5s p95 14.3s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 10 | 1.00 | 0.20 | 0.20 | 0/0 | 0 |
| class | 10 | 1.00 | 0.10 | 0.00 | 0/0 | 0 |
| producer_1 | 10 | 1.00 | 0.20 | 0.10 | 0/0 | 0 |
| producer_2 | 6 | 1.00 | 0.33 | 0.17 | 0/0 | 0 |
| origin | 3 | 1.00 | 0.67 | 0.00 | 0/0 | 0 |
| abv | 10 | 1.00 | 0.10 | 0.10 | 0/0 | 0 |
| net | 10 | 1.00 | 0.20 | 0.00 | 0/0 | 0 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 1/10 (3 reviewed, 6 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 6/7 labels (compliant headers verified and regular-weight headers caught).

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | no alphabet | free-text recall |
|---|---|---|---|
| warning in capitals | 0 | 0 | 0.00 |
| light on dark | 0 | 0 | 0.00 |
| vertical warning | 0 | 0 | 0.00 |
| crowded warning | 0 | 0 | 0.00 |
| none of these | 10 | 3 | 0.23 |

| label | TTB ID | taken, casing | brand class producer_1 producer_2 origin abv net | what the label is like |
|---|---|---|---|---|
| 0001 | 26027001000487 | as_is, — | no alphabet | light type on black; warning in condensed caps |
| 0002 | 26044001000617 | rot90, upper | brand V class — produ V produ V abv — net — | warning vertical; brand in script; class in display face |
| 0003 | 26051001000586 | as_is, — | no alphabet | clean; class and warning large |
| 0004 | 26166001000601 | as_is, as_given | brand — class — produ — abv — net — | warning letterspaced with items on separate lines; producer magenta on dark band |
| 0005 | 26054001000022 | as_is, upper | brand V class — produ V produ — origi V abv — net — | warning condensed caps; label prints "WITHE WINE" |
| 0006 | 26146001000213 | as_is, upper | brand R class V produ — origi V abv R net V | warning condensed caps on back; "ALC 19% by Vol." |
| 0007 | 26188001000248 | inverted, upper | brand R class — produ R produ R abv V net V | single strip label, warning boxed |
| 0008 | 26033001000372 | as_is, — | no alphabet | can wrap; warning condensed caps in a column; beer fill 355 mL |
| 0009 | 26202001000918 | inverted_rot90, upper | brand — class — produ — abv s net — | light type on dark blue; warning vertical and tiny |
| 0010 | 25132001000733 | as_is, as_given | brand — class — produ — produ V origi s abv s net — | gold gradient; warning centered with items on their own lines; "ALC. BY VOL. 5%" |

7 of ten learn an alphabet (5a: six; the rule filter recovers Stokelan and the inversion attempt THE AUSTIN WINERY) and 12 claims verify where 5a verified four: Breckenridge's brand and both producer lines, found in the upright image after its vertical warning was aligned rotated; EDDA's brand, producer, and origin; Stokelan's class, origin, and net; THE AUSTIN WINERY's alcohol content, the first on a real label, and its net; Gallo's importer address. Precision is 1.00 and there is no false mismatch; in one intermediate run "331" cut from a zip code had read as a fill of 331 mL on Gallo at exactly the radius, and a check that every letter of a unit matches within 0.45 of the code now stands, though the fragment was excluded in the final run by the per-reading decision rather than by that check. What still fails on real labels: claims set in bold, condensed, and display faces still align at 0.14 to 0.24 (Seelbach's, Gaul, Gallo's brand and class), which the encoder's training set, regular faces of installed families, does not cover; the latency of a label whose warning is vertical (Breckenridge, 14 s) because two orientations fail before the third, and every region of two images is then searched; and three labels that learn nothing (reversed condensed capitals on black, a distressed face, condensed capitals in a narrow column).

## What the numbers say

Precision of VERIFIED is the number that matters for a compliance tool, and it holds at 0.97 to 1.00 on every claim: the engine does not confirm a wrong value. Where it lacks evidence it says REVIEW or NOT_FOUND. The seven brand verdicts counted against precision are labels whose producer line names the applicant's company with the expected brand words ("Distilled and Bottled by Highland Gate Company" under a brand line reading something else); the engine found the brand text where it genuinely is. A caller that needs the brand on the brand line must say so; the engine verifies text, not layout.

Recall of free-text claims is about 0.70 over the whole set and about 0.95 among labels that learned an alphabet; the gap is the 27 percent of labels that did not. Those are labels blurred until letters fuse (a warning with 240 characters arriving as 70 to 90 components), rotated and compressed until the block does not align, or low in contrast under JPEG noise; they return NOT_FOUND on every claim rather than a guess. A printed class that differs from the application is NOT_FOUND rather than MISMATCH, because free text has no enumeration to name the other value against.

Alcohol content and net contents are decided by digits, and the reference teaches only 1 and 2. Digits synthesized from bundled faces separate a held-out font's digits poorly; alternatives from the three nearest faces and digits learned from claims that verified raise net recall to 0.59 and alcohol content to 0.33 on the reported half, and every wrong net-contents value that decoded at all was called MISMATCH. The remaining alcohol-content claims mostly stop at REVIEW with the two nearest values named, which is the honest outcome when a 7 and a 1 in an unfamiliar face sit within one alphabet spread of each other. This is where the spec's learned encoder belongs: a code trained to keep the same digit together across faces and channel would lift exactly these numbers.

The reference rows verify completely on 38 percent of compliant labels, review on 48, and fail on 14; half of the altered-wording and title-case errors are caught. The threshold sweep at the end of the table shows the trade: a lower failure threshold catches more altered wordings and fails more compliant labels, and no setting separates them well, because a blurred letter and a substituted one look alike to the code. The emphasis test is right on 70 percent of labels. Both would improve with the same encoder.

## Limits, stated

- A brand in a display face outside the alphabet decodes as NOT_FOUND.
- A label without a legible warning block yields no alphabet; every claim is NOT_FOUND with reason `no_alphabet`.
- Heavy blur that fuses letters, steep angles, and low contrast with compression noise degrade alignment; the engine says so rather than guessing.
- Digits are the least reliable glyphs because they are never in the reference; the learned encoder of the spec's stretch section is the remedy if the table below target is not enough.
- Unusual net-contents sizes outside the enumeration decode as REVIEW.
- One alignment slip and one substituted letter look the same; a reference row with a single anomaly goes to review with the glyph as evidence.
