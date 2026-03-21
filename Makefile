BINARY := strspc-manager
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/SteerSpec/strspc-manager/src/internal/version.Version=$(VERSION) \
	-X github.com/SteerSpec/strspc-manager/src/internal/version.Commit=$(COMMIT) \
	-X github.com/SteerSpec/strspc-manager/src/internal/version.Date=$(DATE)

.PHONY: setup build test lint fmt clean

setup:
	@command -v pre-commit >/dev/null 2>&1 || { echo "pre-commit not found — install via: brew install pre-commit"; exit 1; }
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found — install via: brew install golangci-lint"; exit 1; }
	pre-commit install
	pre-commit install --hook-type commit-msg
	@echo "✓ pre-commit hooks installed"

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./src/cmd/strspc-manager

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .
	go run golang.org/x/tools/cmd/goimports@latest -w .

clean:
	rm -f $(BINARY)
