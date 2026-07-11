.PHONY: build test

build:
	go build -o bin/studypilot ./cmd/studypilot

test:
	go test ./...
