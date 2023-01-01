.DEFAULT_GOAL := build
BIN := rescue-api

fmt:
	go fmt ./...

lint: fmt
	@command -v docker >/dev/null || { echo "You need Docker installed to run the linter" && exit 1; }
	docker run --rm -v $(shell pwd):/app -w /app golangci/golangci-lint golangci-lint run -v

test:
	go test ./services

build:
	go build -o $(BIN)

clean:
	rm -rf $(BIN)

.PHONY: fmt lint test build clean
