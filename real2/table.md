## Encoder dual, claims decoded with the learned encoder

50 labels, 10 without an alphabet, latency median 6.4s p95 9.5s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 50 | 1.00 | 0.06 | 0.00 | 0/0 | 0 |
| class | 50 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| producer_1 | 50 | 1.00 | 0.04 | 0.00 | 0/0 | 0 |
| producer_2 | 49 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| origin | 14 | 1.00 | 0.43 | 0.00 | 0/0 | 0 |
| abv | 50 | 1.00 | 0.04 | 0.00 | 0/0 | 0 |
| net | 50 | 1.00 | 0.10 | 0.02 | 0/0 | 0 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 2/50 (13 reviewed, 35 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 21/40 labels (compliant headers verified and regular-weight headers caught).

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | no alphabet | free-text recall |
|---|---|---|---|
| warning in capitals | 0 | 0 | 0.00 |
| light on dark | 0 | 0 | 0.00 |
| vertical warning | 0 | 0 | 0.00 |
| crowded warning | 0 | 0 | 0.00 |
| none of these | 50 | 10 | 0.05 |

