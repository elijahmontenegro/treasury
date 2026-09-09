## Encoder dual, claims decoded with the learned encoder

50 labels, 22 without an alphabet, latency median 21.0s p95 59.3s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 50 | 1.00 | 0.08 | 0.02 | 0/0 | 1 |
| class | 50 | 0.00 | 0.00 | 0.00 | 0/0 | 44 |
| producer_1 | 50 | 1.00 | 0.11 | 0.02 | 0/0 | 30 |
| producer_2 | 50 | 0.00 | 0.00 | 0.00 | 0/0 | 47 |
| origin | 14 | 1.00 | 0.43 | 0.00 | 0/0 | 0 |
| abv | 50 | 1.00 | 0.04 | 0.02 | 0/0 | 2 |
| net | 50 | 1.00 | 0.12 | 0.08 | 0/0 | 1 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 2/50 (25 reviewed, 23 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 18/28 labels (compliant headers verified and regular-weight headers caught).

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | no alphabet | free-text recall |
|---|---|---|---|
| warning in capitals | 0 | 0 | 0.00 |
| light on dark | 0 | 0 | 0.00 |
| vertical warning | 0 | 0 | 0.00 |
| crowded warning | 0 | 0 | 0.00 |
| none of these | 50 | 22 | 0.13 |

