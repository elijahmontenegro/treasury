## Encoder dual, claims decoded with the learned encoder

250 labels, 120 without an alphabet, latency median 17.9s p95 92.1s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 131 | 1.00 | 0.19 | 0.02 | 0/6 | 0 |
| class | 250 | 1.00 | 0.18 | 0.01 | 0/3 | 3 |
| producer_1 | 250 | 1.00 | 0.15 | 0.02 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.21 | 0.01 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.22 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.12 | 0.03 | 0/5 | 3 |
| net | 250 | 1.00 | 0.15 | 0.07 | 1/5 | 5 |
| brand (display face) | 119 | 1.00 | 0.23 | 0.02 | | |

Reference rows: compliant labels with every row verified 17/205 (115 reviewed, 73 failed); wording and title-case errors caught 4/7.
Emphasis: correct on 65/110 labels (compliant headers verified and regular-weight headers caught).

Cross-face gap (correct claims only; same-face = set in the warning's face):

| claim | same-face n | same-face recall | cross-face n | cross-face recall |
|---|---|---|---|---|
| class | 52 | 0.25 | 192 | 0.16 |
| producer_1 | 55 | 0.22 | 195 | 0.13 |
| producer_2 | 55 | 0.29 | 195 | 0.19 |
| origin | 58 | 0.16 | 192 | 0.24 |
| abv | 65 | 0.12 | 177 | 0.12 |
| net | 64 | 0.09 | 176 | 0.16 |

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | no alphabet | free-text recall |
|---|---|---|---|
| warning in capitals | 121 | 51 | 0.22 |
| light on dark | 48 | 22 | 0.25 |
| vertical warning | 41 | 30 | 0.12 |
| crowded warning | 46 | 29 | 0.15 |
| none of these | 72 | 33 | 0.18 |

