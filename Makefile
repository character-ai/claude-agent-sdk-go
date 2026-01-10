.PHONY: all build test fmt lint vet clean examples

all: fmt lint test build

build:
	go build ./...

test:
	go test -v -race ./...

fmt:
	go fmt ./...
	goimports -w .

lint: vet
	golangci-lint run ./...

vet:
	go vet ./...

clean:
	go clean ./...
	rm -f coverage.out

examples:
	go build -o bin/simple ./examples/simple
	go build -o bin/streaming ./examples/streaming
	go build -o bin/tools ./examples/tools
	go build -o bin/generation ./examples/generation

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

deps:
	go mod download
	go mod tidy

check: fmt vet test
