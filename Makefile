.PHONY: build test vet lint tidy clean gen check

BINARY := bin/gossip
PKG    := ./...
GOBIN  := $(shell go env GOBIN)
ifeq ($(GOBIN),)
  GOBIN := $(shell go env GOPATH)/bin
endif

build:
	go build -trimpath -ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" -o $(BINARY) ./cmd/gossip

test:
	go test -race -count=1 $(PKG)

vet:
	go vet $(PKG)

tidy:
	go mod tidy

gen:
	bash scripts/gen-protocol.sh

clean:
	rm -rf bin dist

check: vet test build
