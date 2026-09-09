## The set

250 labels, 0 the reader found nothing on, latency median 1.6s p95 4.8s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 131 | 1.00 | 0.82 | 0.00 | 0/6 | 0 |
| class | 250 | 1.00 | 0.89 | 0.00 | 0/3 | 3 |
| producer_1 | 250 | 1.00 | 0.27 | 0.00 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.36 | 0.00 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.66 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.80 | 0.05 | 1/5 | 3 |
| net | 250 | 1.00 | 0.89 | 0.00 | 4/5 | 5 |
| brand (display face) | 119 | 1.00 | 0.74 | 0.00 | | |

Reference rows: compliant labels with every row verified 0/205 (205 reviewed, 0 failed); wording and title-case errors caught 0/7.
Emphasis: correct on 0/0 labels (compliant headers verified and regular-weight headers caught).

Cross-face gap (correct claims only; same-face = set in the warning's face):

| claim | same-face n | same-face recall | cross-face n | cross-face recall |
|---|---|---|---|---|
| class | 52 | 0.87 | 192 | 0.90 |
| producer_1 | 55 | 0.24 | 195 | 0.28 |
| producer_2 | 55 | 0.31 | 195 | 0.37 |
| origin | 58 | 0.66 | 192 | 0.67 |
| abv | 65 | 0.83 | 177 | 0.79 |
| net | 64 | 0.86 | 176 | 0.90 |

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | nothing read | free-text recall |
|---|---|---|---|
| warning in capitals | 121 | 0 | 0.58 |
| light on dark | 48 | 0 | 0.66 |
| vertical warning | 41 | 0 | 0.58 |
| crowded warning | 46 | 0 | 0.53 |
| none of these | 72 | 0 | 0.60 |

