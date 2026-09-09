## The set

3 labels, 0 the reader found nothing on, latency median 5.0s p95 5.3s

| claim | n | precision | recall | review | mismatch found | not found on missing |
|---|---|---|---|---|---|---|
| brand | 3 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| class | 3 | 0.00 | 0.00 | 0.00 | 0/0 | 3 |
| producer_1 | 3 | 1.00 | 0.67 | 0.00 | 0/0 | 0 |
| producer_2 | 3 | 1.00 | 0.50 | 0.00 | 0/0 | 1 |
| origin | 1 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| abv | 3 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| net | 3 | 1.00 | 1.00 | 0.00 | 0/0 | 0 |
| brand (display face) | 0 | 0.00 | 0.00 | 0.00 | | |

Reference rows: compliant labels with every row verified 0/3 (3 reviewed, 0 failed); wording and title-case errors caught 0/0.
Emphasis: correct on 0/3 labels (compliant headers verified and regular-weight headers caught).

Convention coverage (free-text recall over brand, class, producer, origin):

| convention | labels | nothing read | free-text recall |
|---|---|---|---|
| warning in capitals | 0 | 0 | 0.00 |
| light on dark | 0 | 0 | 0.00 |
| vertical warning | 0 | 0 | 0.00 |
| crowded warning | 0 | 0 | 0.00 |
| none of these | 3 | 0 | 0.78 |

