## Encoder dual, claims decoded with the learned encoder

10 labels, 3 without an alphabet, latency median 5.6s p95 8.7s

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

