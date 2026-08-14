# Thin product Makefile. Engine source stays in nicos-tools.
NICOS_TOOLS ?= $(HOME)/dev/nicos-tools
NDEV        := $(NICOS_TOOLS)/nicos-dev
PREFIX      ?= $(HOME)/.local
BINDIR      ?= $(PREFIX)/bin

.PHONY: test install version help

help:
	@echo "targets: test install version"
	@echo "public install: clone https://github.com/nstranquist/nicos-tools && go build -o ~/.local/bin/ngtm ./cmd/ngtm (from nicos-dev/)"
	@echo "local convenience engine: $(NDEV)/cmd/ngtm"

test:
	cd "$(NDEV)" && go test ./internal/gtm ./internal/gtmcli ./cmd/ngtm ./internal/inference/client -count=1

install:
	cd "$(NDEV)" && go build -o bin/ngtm ./cmd/ngtm
	install -m 755 "$(NDEV)/bin/ngtm" "$(BINDIR)/ngtm"
	@$(BINDIR)/ngtm version

version:
	ngtm version
