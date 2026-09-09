# The residual damage, and a confusion-aware distance (step 27c)

The pairs are the engine's own evidence: what it read, and the spelling it compared
that reading to. A claim that decides nothing has named its nearest reading since
step 25b, so the near misses can be re-scored under a different metric offline.

## The claims still outside the radius, with the damage named

| label | claim | the reading | the spelling | d | what the recogniser did |
|---|---|---|---|---|---|
| 0006 | brand | `Super SPTE` | `SUPER LYTE` | 0.22 | Y read as P, L read as S |
| 0022 | brand | `wls Braw` | `OWL'S BREW` | 0.25 | E read as A |
| 0023 | brand | `wls Braw` | `OWL'S BREW` | 0.25 | E read as A |
| 0024 | brand | `wls Braw` | `OWL'S BREW` | 0.25 | E read as A |
| 0029 | brand | `S URCO S` | `20 SURCOS` | 0.25 | characters dropped only |
| 0032 | brand | `S URCOS` | `20 SURCOS` | 0.25 | characters dropped only |
| 0033 | brand | `Bodegas Y vinedos veG#E us` | `BODEGAS Y VINEDOS VEGA DE YUSO` | 0.16 | characters dropped only |
| 0034 | net | `12 FL. 0Z. *` | `12 FL OZ` | 0.17 | O read as 0 |
| 0039 | abv | `AC40% byvl` | `ALC. 40% BY VOL.` | 0.20 | characters dropped only |
| 0041 | abv | `20 Proof` | `20 Proof` | 0.00 | characters dropped only |
| 0050 | producer_1 | `ImPOrTed by: CRaPEViNE DISTRIB` | `Imported by GRAPEVINE DISTRIBU` | 0.18 | G read as C |

## The confusion set these readings actually show

| the spelling has | the recogniser returned | times |
|---|---|---|
| `E` | `A` | 3 |
| `Y` | `P` | 1 |
| `L` | `S` | 1 |
| `O` | `0` | 1 |
| `G` | `C` | 1 |

## The measurement

Two sets, kept apart. **Systematic** is the shape-confusable set - characters whose
printed forms are near-identical, a property of the alphabet rather than of these
fifty labels. **Observed** adds the three the residual shows that are not of that
kind: E read as A, Y as P, L as S, all from stylised display type on two labels.
Those are one-off misreads and adopting them would be fitting to the report set.

| set | cost of a confusion | carried claims admitted | UNCARRIED claims admitted |
|---|---|---|---|
| systematic | 0.5 | 5 | **0** |
| systematic | 0.0 | 6 | **0** |
| observed | 0.5 | 6 | **0** |
| observed | 0.0 | 10 | **0** |

**systematic at cost 0.5**
- the fifty 0034 net: 0.167 to 0.083, `12 FL. 0Z. *`
- corpus half A 0174 producer_2: 0.158 to 0.132, `Portand, 0regow 97209`
- corpus half A 0312 abv: 0.125 to 0.062, `15% A1c./Vol.`
- corpus half B 0021 abv: 0.143 to 0.000, `39: 5% ABV`
- corpus half B 0273 abv: 0.143 to 0.000, `145% ABV`

**systematic at cost 0.0**
- the fifty 0034 net: 0.167 to 0.000, `12 FL. 0Z. *`
- corpus half A 0174 producer_2: 0.158 to 0.105, `Portand, 0regow 97209`
- corpus half A 0312 abv: 0.125 to 0.000, `15% A1c./Vol.`
- corpus half B 0021 abv: 0.143 to 0.000, `39: 5% ABV`
- corpus half B 0257 brand: 0.200 to 0.133, `Siluer wing's Jron`
- corpus half B 0273 abv: 0.143 to 0.000, `145% ABV`

**observed at cost 0.5**
- the fifty 0006 brand: 0.222 to 0.111, `Super SPTE`
- the fifty 0034 net: 0.167 to 0.083, `12 FL. 0Z. *`
- corpus half A 0174 producer_2: 0.158 to 0.132, `Portand, 0regow 97209`
- corpus half A 0312 abv: 0.125 to 0.062, `15% A1c./Vol.`
- corpus half B 0021 abv: 0.143 to 0.000, `39: 5% ABV`
- corpus half B 0273 abv: 0.143 to 0.000, `145% ABV`

**observed at cost 0.0**
- the fifty 0006 brand: 0.222 to 0.000, `Super SPTE`
- the fifty 0022 brand: 0.250 to 0.125, `wls Braw`
- the fifty 0023 brand: 0.250 to 0.125, `wls Braw`
- the fifty 0024 brand: 0.250 to 0.125, `wls Braw`
- the fifty 0034 net: 0.167 to 0.000, `12 FL. 0Z. *`
- corpus half A 0174 producer_2: 0.158 to 0.105, `Portand, 0regow 97209`
- corpus half A 0312 abv: 0.125 to 0.000, `15% A1c./Vol.`
- corpus half B 0021 abv: 0.143 to 0.000, `39: 5% ABV`
- corpus half B 0257 brand: 0.200 to 0.133, `Siluer wing's Jron`
- corpus half B 0273 abv: 0.143 to 0.000, `145% ABV`

## Whether the measurement is complete

The evidence window is twice the radius: a claim whose nearest reading sat further
than that has no near miss recorded, so it cannot appear above. That is a blind spot
at one cost and not at the other, and the difference is arithmetic rather than luck.

**At a cost of 0.5 the measurement is complete.** Every edit costs at least a half,
so a re-scored distance is at least half the original, and a claim beyond twice the
radius stays beyond the radius. The window and the bound are the same number, so
nothing outside the window could have been admitted.

**At a cost of 0.0 it is not.** A distance can fall to zero however large it was, if
every edit happens to be a confusion, and such a claim would never appear here. The
zero-cost row is therefore reported and not relied on - and it is the wrong rule
anyway: it says O and 0 are the same character, which inside a numeric figure is the
kind of equivalence step 19c added a rule to forbid.
