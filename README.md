# Label verification

Checks whether a drinks label carries what its application filed, and whether it carries the
statutory health warning of 27 CFR 16.21.

It reads the label, compares what it read to what was filed, and answers per claim with the
evidence: the text it read, the spelling it compared that to, how far apart they were, and a crop
of the part of the label the answer rests on. **Where the evidence does not decide, it says so.**
`REVIEW` and `NOT_FOUND` are answers, not failures — the thing this is built to avoid is asserting
something about a label that the label does not bear out.

Everything runs in one process. There is no external model, no API key and no network call at run
time: the reader's weights are embedded in the binary and `/health` reports their SHA-256.
Nothing is stored — an uploaded image lives as long as the request that brought it.

## Setup

Needs Go 1.24 and the ONNX Runtime shared library, version **1.24 or later** (the Go binding asks
for API version 24; older runtimes refuse).

```sh
# Linux
curl -L https://github.com/microsoft/onnxruntime/releases/download/v1.27.1/onnxruntime-linux-x64-1.27.1.tgz | tar xz
mkdir -p third_party/onnxruntime && cp onnxruntime-linux-x64-1.27.1/lib/libonnxruntime.so* third_party/onnxruntime/
```

On Windows put `onnxruntime.dll` in `third_party/onnxruntime/`. Anywhere else, point
`TREASURY_ORT_LIB` at the library.

```sh
go build ./...
go test -short ./...
```

## Run

**The service and its page**

```sh
go run ./cmd/serve            # http://localhost:8080
```

Open it and you get one screen, with two panels. **One label**: pick it, type what the
application filed, press the button.
Results come back as a row per claim — what it is in plain words, the verdict as a word, what was
filed, what the label appears to say, and a picture of where on the label that came from.
**Many labels**: a CSV of claims and a ZIP of images, answered as a table that fills in as each
label is read, a row per label with its claims openable underneath.

**One label from the command line**

```sh
go run ./cmd/decode -ttb real2/0047.json real2/0047.png
```

**In a container**

```sh
docker build -t treasury \
  --build-arg COMMIT=$(git rev-parse HEAD) \
  --build-arg COMMIT_TIME=$(git show -s --format=%cI HEAD) .
docker run --read-only -p 8080:8080 treasury
```

Distroless, non-root, read-only filesystem, 117 MB. It listens on `PORT`.

## Endpoints

The contract is [`api/openapi.yaml`](api/openapi.yaml), written by hand; the types and the server
interface are generated from it, and CI fails if the two have drifted. The running service serves
the document at `/openapi.yaml`.

| | |
|---|---|
| `GET /` | the operator's page |
| `POST /verify` | multipart: `image` (PNG or JPEG) and `claims` (JSON). Verdicts, evidence, crops, timings, build identity |
| `POST /verify/batch` | multipart: `claims` (CSV) and `images` (ZIP). `application/x-ndjson`, one line per label as it finishes |
| `GET /health` | readiness and which build is answering, with the model hashes |
| `GET /openapi.yaml` | the specification |

```sh
curl -X POST localhost:8080/verify \
  -F image=@real2/0047.png \
  -F claims=@real2/0047.json

curl -X POST localhost:8080/verify/batch \
  -F claims=@claims.csv \
  -F images=@labels.zip
```

The batch CSV's first row is its column names. One column must be `image`, naming a file inside
the ZIP; the rest may be `beverage`, `brand`, `class`, `producer`, `address`, `origin`, `abv` and
`net_ml`. Answers stream — a line is written as each label finishes rather than the whole batch
being assembled at the end, so a caller sees progress and nothing accumulates in memory. Verdicts
come back without crops, since three hundred labels of them would be a response measured in
gigabytes; each says whether one exists, and `/verify` will show it for that label alone.

## What it verifies

Brand, class or type, the permittee and its address, origin, alcohol content, net contents, and
the statutory warning with its heading.

A claim is compared to the label's own text, not to a guess about it. The rules that decide are
the same seven the engine has accumulated under measurement, each one added because a specific
false assertion made it necessary — a number must be read exactly as a whole run of digits, a name
taken from inside a longer line must be delimited at both ends and must be the whole name, a
reading with characters missing may not contradict, and so on. `docs/approach.md` records each
with the label that forced it.

## Layout

```
verify/          the decision: distance, margin, refusal, evidence
internal/ocr/    the reader: PP-OCRv4 detection and recognition, embedded, run on the CPU
ttb/             the domain: what the regulation says, and nothing about mechanism
api/             the specification, and the code generated from it
internal/httpapi/  the service and the page
cmd/serve        the service          cmd/decode  one label
cmd/eval         the measurement      cmd/whymissed  where a claim was lost
docs/approach.md every step in the order it was measured
```

## Where the numbers are

`docs/approach.md` carries every table in the order it was measured, including the ones that were
later corrected, and says which. The short version: on fifty real registry labels the engine
verifies 156 of the 192 claims they carry, with **precision 1.00 across 550 labels and not one
false assertion**; median 1.3 s and 95th percentile 3.9 s per label on four cores.
