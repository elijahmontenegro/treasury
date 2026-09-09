## The set

250 labels, 0 the reader found nothing on, latency median 0.9s p95 1.7s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 131 | 1.00 | 0.74 | 0.00 | 0/6 | 0 |
| class | 250 | 1.00 | 0.81 | 0.00 | 0/3 | 3 |
| producer_1 | 250 | 1.00 | 0.18 | 0.00 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.26 | 0.00 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.55 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.79 | 0.03 | 2/5 | 3 |
| net | 250 | 1.00 | 0.86 | 0.00 | 3/5 | 5 |
| brand (display face) | 119 | 1.00 | 0.59 | 0.00 | | |

Reference rows: compliant labels with every row verified 0/205 (205 reviewed, 0 failed); wording and title-case errors caught 0/7.
Emphasis: correct on 0/0 labels (compliant headers verified and regular-weight headers caught).

Cross-face gap (correct claims only; same-face = set in the warning's face):

| claim | same-face n | same-face recall | cross-face n | cross-face recall |
|---|---|---|---|---|
| class | 52 | 0.77 | 192 | 0.82 |
| producer_1 | 55 | 0.16 | 195 | 0.18 |
| producer_2 | 55 | 0.22 | 195 | 0.27 |
| origin | 58 | 0.52 | 192 | 0.56 |
| abv | 65 | 0.82 | 177 | 0.78 |
| net | 64 | 0.86 | 176 | 0.86 |

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | nothing read | free-text recall |
|---|---|---|---|
| warning in capitals | 121 | 0 | 0.47 |
| light on dark | 48 | 0 | 0.56 |
| vertical warning | 41 | 0 | 0.46 |
| crowded warning | 46 | 0 | 0.45 |
| none of these | 72 | 0 | 0.52 |

