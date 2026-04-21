.PHONY: build test vet lint tidy clean gen check \
        release release-all release-darwin release-linux \
        checksums

BINARY  := bin/gossip
PKG     := ./...
DIST    := dist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath -ldflags="$(LDFLAGS)"

GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
  GOBIN := $(shell go env GOPATH)/bin
endif

build:
	go build $(GOFLAGS) -o $(BINARY) ./cmd/gossip

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

# ---------- release / cross-compile ----------

# release-binary OS ARCH
define release-binary
	@mkdir -p $(DIST)/gossip_$(1)_$(2)
	@echo "→ building $(1)/$(2)"
	GOOS=$(1) GOARCH=$(2) CGO_ENABLED=0 go build $(GOFLAGS) \
		-o $(DIST)/gossip_$(1)_$(2)/gossip ./cmd/gossip
	@cp README.md LICENSE $(DIST)/gossip_$(1)_$(2)/ 2>/dev/null || true
	tar -czf $(DIST)/gossip_$(VERSION:v%=%)_$(1)_$(2).tar.gz -C $(DIST) gossip_$(1)_$(2)
endef

release-darwin:
	$(call release-binary,darwin,arm64)
	$(call release-binary,darwin,amd64)

release-linux:
	$(call release-binary,linux,amd64)
	$(call release-binary,linux,arm64)

release-all: release-darwin release-linux checksums
	@echo ""
	@echo "→ artifacts in $(DIST)/"
	@ls -1 $(DIST) | grep -E '\.tar\.gz$$' | sed 's/^/    /'

release: release-all

checksums:
	@cd $(DIST) && \
	  (command -v sha256sum >/dev/null 2>&1 && sha256sum *.tar.gz > checksums.txt 2>/dev/null) \
	  || (command -v shasum   >/dev/null 2>&1 && shasum -a 256 *.tar.gz > checksums.txt 2>/dev/null) \
	  || true
	@[ -s $(DIST)/checksums.txt ] && echo "→ wrote $(DIST)/checksums.txt" || echo "→ no checksums (no archives?)"
