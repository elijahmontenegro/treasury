## The set

50 labels, 0 the reader found nothing on, latency median 1.0s p95 1.7s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 50 | 1.00 | 0.35 | 0.00 | 0/0 | 1 |
| class | 50 | 0.00 | 0.00 | 0.00 | 0/0 | 44 |
| producer_1 | 50 | 1.00 | 0.26 | 0.00 | 0/0 | 31 |
| producer_2 | 50 | 1.00 | 0.33 | 0.00 | 0/0 | 47 |
| origin | 14 | 1.00 | 0.64 | 0.00 | 0/0 | 0 |
| abv | 50 | 1.00 | 0.40 | 0.00 | 0/0 | 0 |
| net | 50 | 1.00 | 0.22 | 0.00 | 0/0 | 0 |
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
| none of these | 50 | 0 | 0.35 |

