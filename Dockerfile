# syntax=docker/dockerfile:1.6

FROM golang:1.22-bookworm AS builder
WORKDIR /src

# libvips + build deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    libvips-dev pkg-config git && rm -rf /var/lib/apt/lists/*

COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags "-s -w" -o /out/api  ./cmd/api
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags "-s -w" -o /out/worker ./cmd/worker

# ---- runtime ----
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libvips42 curl && rm -rf /var/lib/apt/lists/*

# ONNX Runtime shared library
ARG ORT_VERSION=1.17.3
RUN mkdir -p /opt/onnxruntime && \
    curl -L "https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/onnxruntime-linux-x64-${ORT_VERSION}.tgz" \
    | tar -xz -C /opt/onnxruntime --strip-components=1
ENV LD_LIBRARY_PATH=/opt/onnxruntime/lib

WORKDIR /app
COPY --from=builder /out/api    /app/api
COPY --from=builder /out/worker /app/worker
COPY model /internal/model
EXPOSE 8080
ENV PORT=8080
CMD ["/app/api"]
