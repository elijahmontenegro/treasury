## The set

50 labels, 0 the reader found nothing on, latency median 2.5s p95 4.7s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 50 | 1.00 | 0.51 | 0.00 | 0/0 | 1 |
| class | 50 | 1.00 | 0.17 | 0.00 | 0/0 | 44 |
| producer_1 | 50 | 1.00 | 0.68 | 0.00 | 0/0 | 31 |
| producer_2 | 50 | 1.00 | 0.33 | 0.00 | 0/0 | 47 |
| origin | 14 | 1.00 | 0.93 | 0.00 | 0/0 | 0 |
| abv | 50 | 1.00 | 0.92 | 0.02 | 0/0 | 0 |
| net | 50 | 1.00 | 0.94 | 0.00 | 0/0 | 0 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 0/50 (50 reviewed, 0 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 0/0 labels (compliant headers verified and regular-weight headers caught).

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | nothing read | free-text recall |
|---|---|---|---|
| warning in capitals | 0 | 0 | 0.00 |
| light on dark | 0 | 0 | 0.00 |
| vertical warning | 0 | 0 | 0.00 |
| crowded warning | 0 | 0 | 0.00 |
| none of these | 50 | 0 | 0.58 |

