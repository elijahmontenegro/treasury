## The set

250 labels, 0 the reader found nothing on, latency median 1.6s p95 3.5s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 129 | 1.00 | 0.86 | 0.00 | 0/3 | 0 |
| class | 250 | 1.00 | 0.89 | 0.00 | 0/3 | 6 |
| producer_1 | 250 | 1.00 | 0.40 | 0.00 | 0/0 | 0 |
| producer_2 | 250 | 1.00 | 0.47 | 0.00 | 0/0 | 0 |
| origin | 250 | 1.00 | 0.81 | 0.00 | 0/0 | 0 |
| abv | 250 | 1.00 | 0.85 | 0.01 | 1/3 | 8 |
| net | 250 | 1.00 | 0.82 | 0.00 | 4/6 | 6 |
| brand (display face) | 121 | 1.00 | 0.87 | 0.00 | | |

Reference rows: compliant labels with every row verified 0/198 (198 reviewed, 0 failed); wording and title-case errors caught 0/11.
Emphasis: correct on 0/0 labels (compliant headers verified and regular-weight headers caught).

Cross-face gap (correct claims only; same-face = set in the warning's face):

| claim | same-face n | same-face recall | cross-face n | cross-face recall |
|---|---|---|---|---|
| class | 55 | 0.82 | 186 | 0.91 |
| producer_1 | 59 | 0.41 | 191 | 0.39 |
| producer_2 | 59 | 0.41 | 191 | 0.49 |
| origin | 59 | 0.80 | 191 | 0.82 |
| abv | 61 | 0.87 | 178 | 0.85 |
| net | 60 | 0.77 | 178 | 0.84 |

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | nothing read | free-text recall |
|---|---|---|---|
| warning in capitals | 119 | 0 | 0.69 |
| light on dark | 51 | 0 | 0.68 |
| vertical warning | 47 | 0 | 0.67 |
| crowded warning | 56 | 0 | 0.62 |
| none of these | 64 | 0 | 0.69 |

