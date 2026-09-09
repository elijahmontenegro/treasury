## The set

2 labels, 0 the reader found nothing on, latency median 2.5s p95 2.5s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 2 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| class | 2 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| producer_1 | 2 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| producer_2 | 2 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| origin | 0 | 0.00 | 0.00 | 0.00 | 0/0 | 0 |
| abv | 2 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| net | 2 | 1.00 | 0.50 | 0.00 | 0/0 | 0 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 0/2 (2 reviewed, 0 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 0/2 labels (compliant headers verified and regular-weight headers caught).

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | nothing read | free-text recall |
|---|---|---|---|
| warning in capitals | 0 | 0 | 0.00 |
| light on dark | 0 | 0 | 0.00 |
| vertical warning | 0 | 0 | 0.00 |
| crowded warning | 0 | 0 | 0.00 |
| none of these | 2 | 0 | 1.00 |

