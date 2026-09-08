The fifty carry 191 claims; 120 verify and 71 do not.

| cause | claims | brand | class | producer 1 | producer 2 | origin | alcohol | net |
|---|---|---|---|---|---|---|---|---|
| not detected at all | **26** | 10 | 1 | 5 | 2 | 1 | 3 | 4 |
| inside more of the same kind of text, where the rule is right | **21** | 14 | 4 | 2 | 1 | - | - | - |
| detected and misread | **9** | 4 | - | 5 | - | - | - | - |
| printed in a form the enumeration lacks | **7** | - | - | - | - | - | 6 | 1 |
| read correctly, beside another statement in one detection | **4** | - | - | - | - | 4 | - | - |
| matched, outside the radius | **2** | 2 | - | - | - | - | - | - |
| read correctly, but split across detections | **1** | 1 | - | - | - | - | - | - |
| refused by one of the four rules | **1** | - | - | - | - | - | 1 | - |
| **all** | **71** | 31 | 5 | 12 | 3 | 5 | 10 | 5 |

### not detected at all
  0002 producer_1  run=0.64 box=0.59 seq=0.59(6) NOT_FOUND | box='According  to  the' | run='(1) According  to  the'
  0002 producer_2  run=0.81 box=0.78 seq=0.78(7) NOT_FOUND | box='machinery, and may cause health' | run='General,'
  0003 producer_1  run=0.72 box=0.69 seq=0.65(5) NOT_FOUND | box='Douo is paiol (aneasaid e se aeozuag' | run='Alc. 5% by vol. Net Cont. 475 ml'
  0003 producer_2  run=0.81 box=0.81 seq=0.74(16) NOT_FOUND | box='HECHO ENMEXICO' | run='ui dnog up zo :a paodui'
  0006 brand       run=0.56 box=0.44 seq=0.44(5) NOT_FOUND | box='OPERATEMACHINERYANDMAY' | run='SPIRE'
  0013 brand       run=0.56 box=0.44 seq=0.44(3) NOT_FOUND | box='OPERATE MACHINERY,AND MAY' | run='SPIRE'
  0014 brand       run=0.56 box=0.44 seq=0.44(2) NOT_FOUND | box='OPERATE MACHINERY AND MAY' | run='SPIRE'
  0018 brand       run=0.67 box=0.44 seq=0.44(3) NOT_FOUND | box='OPERATEMACHINERYAND MAY' | run='NSPIRET'
  0019 brand       run=0.56 box=0.44 seq=0.44(2) NOT_FOUND | box='OPERATEMACHINERY,ND MAY' | run='SPIRE'
  0020 brand       run=0.56 box=0.44 seq=0.44(2) NOT_FOUND | box='OPERATEMACHINERYANDMAY' | run='SPIRE'
  0021 brand       run=0.56 box=0.44 seq=0.44(3) NOT_FOUND | box='OPERATE MACHINERY,AND MAY' | run='SPIRE'
  0026 brand       run=0.67 box=0.75 seq=0.58(9) NOT_FOUND | box='NDA' | run='E A NDA'
  0026 class       run=0.81 box=0.81 seq=0.62(14) NOT_FOUND | box='BOURD' | run='BOURD'
  0026 net         run=0.60 box=0.60 seq=0.60(6) NOT_FOUND | box='50' | run='50'
  0028 abv         run=0.75 box=0.67 seq=0.62(5) NOT_FOUND | box='2025 CHARDONNAY' | run='2025'
  0028 net         run=0.62 box=0.60 seq=0.60(2) NOT_FOUND | box='Fermented and aged 7 months' | run='LIVE'
  0034 brand       run=0.71 box=0.71 seq=0.54(11) NOT_FOUND | box='BIG CREEK' | run='BIG CREEK'
  0034 producer_1  run=0.71 box=0.71 seq=0.51(13) NOT_FOUND | box='BIG CREEK' | run='BLONDEALE HONEY'
  0035 producer_1  run=0.62 box=0.44 seq=0.44(2) NOT_FOUND | box='VINEYARDS' | run='VINEYARDS'
  0037 abv         run=0.75 box=0.44 seq=0.44(2) SKIPPED | box='ALCOHOLIC BEVERAGES DURING PREGNANCY' | run='PREMIUM'
  0043 net         run=0.75 box=0.50 seq=0.50(6) NOT_FOUND | box='KNOWLTON' | run='ICRAFTED'
  0043 producer_1  run=0.65 box=0.65 seq=0.52(11) NOT_FOUND | box='KNOWLTON' | run='KNOWLTON'
  0044 abv         run=0.71 box=0.70 seq=0.65(4) NOT_FOUND | box="To'oa %'l' njo onpd 'sns sueu" | run='STRONG'
  0044 brand       run=0.64 box=0.64 seq=0.57(5) NOT_FOUND | box='uou aeado loie eaupoiigeino sdu' | run='RED BLEND'
  0044 net         run=0.75 box=0.71 seq=0.62(2) NOT_FOUND | box='uou aeado loie eaupoiigeino sdu' | run='40'
  0044 origin      run=0.75 box=0.50 seq=0.50(2) SKIPPED | box='uup ou pnous uaom' | run='STRONG TRUE'

### inside more of the same kind of text, where the rule is right
  0012 class       run=0.67 box=0.00 seq=0.00(4) NOT_FOUND | box='INDIA PALE ALE' | run='ages'
  0015 brand       run=0.50 box=0.00 seq=0.00(14) NOT_FOUND | box='BREWEDANCANNEDWASATCHBREWERYSALTLAKEIU' | run='WASATCH'
  0015 class       run=1.00 box=0.00 seq=0.00(9) NOT_FOUND | box='STARGAZE-INDIA PALE ALE' | run=''
  0016 brand       run=0.50 box=0.00 seq=0.00(2) NOT_FOUND | box='BREWEDANDCANNEDBYWASATCHBREWERY-SALLAKEC' | run='WASATCH'
  0016 class       run=1.00 box=0.00 seq=0.00(2) NOT_FOUND | box='GHOSTRIDERINDIA PALEALE' | run=''
  0017 brand       run=0.50 box=0.00 seq=0.00(2) NOT_FOUND | box='BREWEDANDCANNEDBYWASATCHBREWERY-SALLAKEC' | run='WASATCH'
  0017 class       run=0.67 box=0.00 seq=0.00(2) NOT_FOUND | box='HOLY HAZE M-HAZY PALE ALE' | run='HAZE'
  0022 brand       run=0.62 box=0.00 seq=0.00(3) NOT_FOUND | box='followusGtheowlsbrew' | run='wls'
  0023 brand       run=0.62 box=0.00 seq=0.00(3) NOT_FOUND | box='followus @theowlsbrew' | run='wls'
  0024 brand       run=0.62 box=0.00 seq=0.00(1) NOT_FOUND | box="Iteamedupwith Owl'sBrew to" | run='wls'
  0025 brand       run=0.62 box=0.00 seq=0.00(1) NOT_FOUND | box="IteamedupwithOwl'sBrew to" | run='Braw'
  0028 brand       run=0.21 box=0.00 seq=0.00(4) NOT_FOUND | box='APONA VINEYARDS, VENETA, OR' | run='APONAVINEYARDS.COM'
  0037 producer_2  run=0.10 box=0.00 seq=0.00(3) NOT_FOUND | box='1944GardenaAve,Glendale,CA91204USA' | run='1944GardenaAve,Glendale,CA91204USA'
  0038 brand       run=0.33 box=0.00 seq=0.00(1) NOT_FOUND | box='Distilled & Bottled by 45th Parallel Spi' | run='Parallel'
  0041 brand       run=0.38 box=0.00 seq=0.00(2) NOT_FOUND | box='and Peaky Blinders partnership, this spi' | run='BLINDERS'
  0041 producer_1  run=0.12 box=0.00 seq=0.00(2) NOT_FOUND | box='Bottled By Bluegrass Bottling, Lancaster' | run='Bottled By Bluegrass Bottling, Lan'
  0042 brand       run=0.71 box=0.00 seq=0.00(1) NOT_FOUND | box='spirit of Alpas Vineyards and the' | run='AS NE'
  0042 producer_1  run=0.12 box=0.00 seq=0.00(3) NOT_FOUND | box='Engelheim Vineyards, Ellijay, Georgia' | run='Produced and Bottled by Engelheim '
  0043 brand       run=0.29 box=0.00 seq=0.00(3) NOT_FOUND | box='ID TENHEAD' | run='ID TENHEAD'
  0046 brand       run=0.71 box=0.00 seq=0.00(2) NOT_FOUND | box='Bottled by: PASSIONE NATURA, Paglieta (C' | run='BENVENUSA'
  0048 brand       run=0.63 box=0.00 seq=0.00(3) NOT_FOUND | box='SCEA CHATEAU COTEDE BALEAU,PROPRIETAIRE' | run='Chateau'

### detected and misread
  0001 producer_1  run=0.37 box=0.31 seq=0.31(3) NOT_FOUND | box='FAMILY WINES AND' | run='FAMILY WINES AND'
  0007 brand       run=0.45 box=0.45 seq=0.36(4) NOT_FOUND | box='JOHNNY' | run='JOHNNY'
  0029 brand       run=0.38 box=0.38 seq=0.38(4) NOT_FOUND | box='S URCO' | run='s S URCO'
  0030 brand       run=0.38 box=0.38 seq=0.38(3) NOT_FOUND | box='S URCO' | run='S URCO'
  0031 brand       run=0.38 box=0.38 seq=0.38(1) NOT_FOUND | box='SUROOS' | run='SUROOS'
  0047 producer_1  run=0.40 box=0.40 seq=0.35(4) NOT_FOUND | box='DISTrIbUTors CoNCord, NC' | run='DISTrIbUTors CoNCord, NC'
  0048 producer_1  run=0.40 box=0.40 seq=0.35(4) NOT_FOUND | box='DISTrIbUtors Concord, NC' | run='DISTrIbUtors Concord, NC'
  0049 producer_1  run=0.40 box=0.40 seq=0.35(4) NOT_FOUND | box='DISTrIBUToRS CoNCOrD, NC' | run='DISTrIBUToRS CoNCOrD, NC'
  0050 producer_1  run=0.42 box=0.42 seq=0.35(6) NOT_FOUND | box='ImPOrTed by: CRaPEViNE' | run='ImPOrTed by: CRaPEViNE'

### printed in a form the enumeration lacks
  0004 abv         run=0.25 box=0.25 seq=0.25(3) NOT_FOUND | box='40%alcl' | run='40%alcl'
  0005 abv         run=0.25 box=0.25 seq=0.25(3) NOT_FOUND | box='40% alcl.' | run='40% alcl.'
  0026 abv         run=0.60 box=0.60 seq=0.57(8) NOT_FOUND | box='50' | run='50'
  0034 net         run=0.75 box=0.17 seq=0.17(4) NOT_FOUND | box='12 FL. 0Z. * ALC. 5.5%o BY VOL.' | run='TUPELO'
  0036 abv         run=0.25 box=0.25 seq=0.12(5) NOT_FOUND | box='%ALC. /VOL' | run='%ALC. /VOL'
  0039 abv         run=0.30 box=0.20 seq=0.20(5) NOT_FOUND | box='AC40% byvlR' | run='AC40% byvlR'
  0050 abv         run=0.68 box=0.25 seq=0.25(1) NOT_FOUND | box='NC 750 ML 13.5% ALC. BY' | run='NC 750 ML 13.5% ALC. BY'

### read correctly, beside another statement in one detection
  0001 origin      run=0.40 box=0.00 seq=0.00(5) SKIPPED | box='4 PRODUCTOFMEXICO 750 ML' | run='4 PRODUCTOFMEXICO 750 ML'
  0033 origin      run=0.50 box=0.00 seq=0.00(3) SKIPPED | box='Red Wine - Product of Spain' | run='Red Wine - Product of Spain'
  0045 origin      run=0.64 box=0.00 seq=0.00(1) SKIPPED | box='WHITE WINE - PRODUCT OF ITALY' | run='WHITE WINE - PRODUCT OF ITALY'
  0046 origin      run=0.64 box=0.00 seq=0.00(2) SKIPPED | box='WHITE WINE - PRODUCT OF ITALY' | run='WHITE WINE - PRODUCT OF ITALY'

### matched, outside the radius
  0032 brand       run=0.25 box=0.25 seq=0.25(3) NOT_FOUND | box='S URCOS' | run='S URCOS'
  0045 brand       run=0.71 box=0.07 seq=0.07(2) NOT_FOUND | box='Bottled by: PASSONE NATURA, Paglieta (CH' | run='BENVENUSA'

### read correctly, but split across detections
  0033 brand       run=0.16 box=0.16 seq=0.00(2) NOT_FOUND | box='Produced and Elaborated by BODEGAS YVINE' | run='Bodegas Y vinedos veG#E us'

### refused by one of the four rules
  0041 abv         run=0.12 box=0.25 seq=0.12(5) REVIEW | box='.% alc/vol.' | run='6D .% alc/vol.'
