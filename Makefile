BINARY  := conduct
BIN_DIR := bin
PKG     := github.com/qoggy/conduct
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X $(PKG)/internal/cli.version=$(VERSION)

# go install 的落点：GOBIN 优先，未设置则回退到 GOPATH/bin
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: build install uninstall test vet fmt clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/conduct

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/conduct

uninstall:
	rm -f "$(GOBIN)/$(BINARY)"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -rf $(BIN_DIR)
