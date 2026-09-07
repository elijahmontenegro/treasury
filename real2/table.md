## Encoder dual

50 labels, 22 without an alphabet, latency median 2.2s p95 10.2s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 50 | 1.00 | 0.04 | 0.00 | 0/0 | 1 |
| class | 50 | 0.00 | 0.00 | 0.00 | 0/0 | 44 |
| producer_1 | 50 | 0.50 | 0.05 | 0.00 | 0/0 | 30 |
| producer_2 | 50 | 0.00 | 0.00 | 0.00 | 0/0 | 47 |
| origin | 14 | 1.00 | 0.21 | 0.00 | 0/0 | 0 |
| abv | 50 | 0.50 | 0.02 | 0.02 | 0/0 | 2 |
| net | 50 | 0.67 | 0.09 | 0.14 | 0/0 | 1 |
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
| none of these | 50 | 22 | 0.07 |

