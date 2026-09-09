# The corpus with split statements (step 27d)

## How much joining a label now needs

| | runs chained per detection | verified claims matched across a chain | labels with at least one |
|---|---|---|---|
| the fifty | 3.47 | 0.45 | 0.70 |
| corpus half A, before | 1.70 | 0.25 | 0.68 |
| corpus half A, after | 2.12 | 0.52 | 0.90 |

Chain lengths, the fifty then the split corpus: {1: 85, 2: 29, 3: 13, 4: 27} and {1: 556, 2: 396, 3: 27, 4: 190}.

## Transfer, corpus half B against the fifty

| claim | the fifty | half B before the split | half B after | gap after | within 0.15 |
|---|---|---|---|---|---|
| brand | 0.55 | 0.76 | 0.86 | 0.31 | **no** |
| class | 0.43 | 0.87 | 0.89 | 0.46 | **no** |
| producer_1 | 0.84 | 0.27 | 0.40 | 0.45 | **no** |
| producer_2 | 0.67 | 0.36 | 0.47 | 0.20 | **no** |
| origin | 0.93 | 0.66 | 0.81 | 0.12 | yes |
| abv | 0.92 | 0.78 | 0.84 | 0.08 | yes |
| net | 0.94 | 0.85 | 0.82 | 0.12 | yes |

3 of 7 within the tolerance stated before the run. The split moved 4 of 7 toward
the fifty and 3 away.
