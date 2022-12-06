.DEFAULT_GOAL := build
BIN := rescue-api

fmt:
	go fmt ./...

lint: fmt
	staticcheck ./...

vet: fmt
	go vet ./...

test:
	go test ./services

build: vet
	go build -o $(BIN)

clean:
	rm -rf $(BIN)

.PHONY: fmt lint vet test build clean
