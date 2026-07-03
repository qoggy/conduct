BINARY  := conduct
BIN_DIR := bin
PKG     := github.com/qoggy/conduct
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X $(PKG)/internal/cli.version=$(VERSION)

.PHONY: build install test vet fmt clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/conduct

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/conduct

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -rf $(BIN_DIR)
