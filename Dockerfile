FROM golang:1.19-bullseye AS builder
COPY . /src
RUN set -eu \
    && cd /src \
    && apt-get install -y make \
    && make build

FROM debian:bullseye-slim
COPY --from=builder /src/rescue-api /app/rescue-api
RUN set -eu \
    && apt-get update \
    && apt-get install -y ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ENTRYPOINT ["/app/rescue-api"]
