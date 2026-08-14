# Product-tree Makefile. Engine lives in this module.
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: test install version help

help:
	@echo "targets: test install version"
	@echo "public product: https://github.com/nstranquist/ngtm"

test:
	go test ./...

install:
	go build -o .bin/ngtm ./cmd/ngtm
	install -m 755 .bin/ngtm "$(BINDIR)/ngtm"
	@"$(BINDIR)/ngtm" version

version:
	go run ./cmd/ngtm version
