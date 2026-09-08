# Does the label print the filed value alone somewhere else? (step 27b)

The test is the engine's own: a span counts as printed alone only where the claim
fills a whole detection, or a mark, a digit or the detection's edge stands at both
ends of it, or across two adjacent detections, since a printed line the detector
cut in half is still printed alone. Every detection is searched, and the reading
is the engine own: 1600 px with the turned pass, not cmd/read old 960 px
upright-only default, which had made 0043 brand look unread.


**The eight**

| label | claim | filed | printed alone elsewhere? | where |
|---|---|---|---|---|
| 0024 | brand | `OWL'S BREW` | no | nearest anywhere: `Iteamedupwith Owl'sBrew to` |
| 0025 | brand | `OWL'S BREW` | no | nearest anywhere: `IteamedupwithOwl'sBrew to` |
| 0034 | brand | `THE CROSSING AT BIG CREEK ` | no | nearest anywhere: `CANNED By THE CROSSING AT BIG CREEK BREWERY` |
| 0042 | brand | `ALPAS VINEYARDS` | no | nearest anywhere: `spirit of Alpas Vineyards and the` |
| 0043 | brand | `TENHEAD` | no | nearest anywhere: `ID TENHEAD` |
| 0044 | brand | `NOTRE DAME WINES` | no | nearest anywhere: `Bottled by Vinovae, Sonoma, CA for Notre Dam` |
| 0044 | origin | `Product of USA` | no | nearest anywhere: `Contains sulfites, Product of USA ALC.14,5% ` |
| 0048 | brand | `CHATEAU COTE DE BALEAU` | no | nearest anywhere: `SCEA CHATEAU COTEDE BALEAU,PROPRIETAIRE` |

**The three the refusal exists for**

| label | claim | filed | printed alone elsewhere? | where |
|---|---|---|---|---|
| 0038 | brand | `45TH PARALLEL` | no | nearest anywhere: `Distilled & Bottled by 45th Parallel Spirits` |
| 0099 | brand | `Valley Mill` | no | nearest anywhere: `Li` |
| 0309 | brand | `HERON BLACK` | no | nearest anywhere: `PROBLEMS.` |
