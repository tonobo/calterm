BINARY := calterm
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test lint fmt install clean

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) ./cmd/calterm

test:
	go test ./... -race

lint:
	go vet ./...

fmt:
	gofmt -w .

install:
	go install -ldflags "-X main.Version=$(VERSION)" ./cmd/calterm

clean:
	rm -f $(BINARY)
