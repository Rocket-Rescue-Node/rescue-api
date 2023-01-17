FROM golang:1.19-bullseye AS builder

COPY . /src

RUN set -eu \
    && cd /src \
    && apt-get install -y make \
    && make build

FROM debian:bullseye-slim
COPY --from=builder /src/rescue-api /app/rescue-api

ENTRYPOINT ["/app/rescue-api"]
