PREFIX ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: install clean

install:
	go build -ldflags "-X main.version=$(VERSION)" -o $(PREFIX)/synapse ./cmd/synapse

clean:
	go clean -cache
