# The service as one container: a static-ish Go binary, the ONNX Runtime
# shared library it loads, and nothing else. The models are embedded in the
# binary, so there is no model download at start and no volume to mount —
# what runs is what was built, and `/health` reports the SHA-256 of the
# weights it is carrying.

# The ONNX Runtime release the binary loads. It is pinned rather than
# floating: the model hashes in the build identity say which weights ran,
# and this says which runtime ran them. It must be at least 1.24: the Go
# binding asks for API version 24, and an older runtime refuses with
# "The requested API version [24] is not available", which is how 1.20.1
# was found to be wrong here.
ARG ORT_VERSION=1.27.1

# ---- build -----------------------------------------------------------------
FROM golang:1.24-bookworm AS build
ARG ORT_VERSION
ARG TARGETARCH=amd64

WORKDIR /src

# The runtime library, fetched here so the build is reproducible from the
# Dockerfile alone. The Windows copy in third_party/ is for development and
# is not in the repository.
ADD https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/onnxruntime-linux-x64-${ORT_VERSION}.tgz /tmp/ort.tgz
RUN tar -xzf /tmp/ort.tgz -C /tmp \
    && mkdir -p /out/lib \
    && cp /tmp/onnxruntime-linux-x64-${ORT_VERSION}/lib/libonnxruntime.so* /out/lib/ \
    && rm -rf /tmp/ort.tgz /tmp/onnxruntime-linux-x64-${ORT_VERSION}

# Dependencies first, so a change to the source does not refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# cgo is required: the reader calls ONNX Runtime through it.
# The commit is passed in rather than read from .git, because the build
# context excludes a 116 MB repository history to stamp two strings. The
# fallback in internal/buildid is used only where the tool chain gave
# nothing, so a build that can read its own history is never overridden.
ARG COMMIT=unknown
ARG COMMIT_TIME=

ENV CGO_ENABLED=1
RUN go build -trimpath       -ldflags="-s -w -X treasury/internal/buildid.Commit=${COMMIT} -X treasury/internal/buildid.CommitTime=${COMMIT_TIME}"       -o /out/serve ./cmd/serve

# ---- run -------------------------------------------------------------------
# cc-debian12 rather than base: ONNX Runtime needs libstdc++ and libgcc,
# which base does not carry. `nonroot` runs as uid 65532 and owns nothing
# it can write to, which is the point.
FROM gcr.io/distroless/cc-debian12:nonroot

COPY --from=build /out/lib/ /usr/lib/
COPY --from=build /out/serve /serve

# Cloud Run sets PORT; the binary reads it, and falls back to 8080.
ENV PORT=8080
EXPOSE 8080

# The library is found at /usr/lib by ocr.LibraryPath's own search, but
# saying so here means a changed search order cannot silently break the
# container.
ENV TREASURY_ORT_LIB=/usr/lib/libonnxruntime.so

USER nonroot:nonroot
ENTRYPOINT ["/serve"]
