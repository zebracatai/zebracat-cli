BINARY  := zebracat
PKG     := github.com/zebracatai/zebracat-cli
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X $(PKG)/internal/version.Version=$(VERSION) \
  -X $(PKG)/internal/version.Commit=$(COMMIT) \
  -X $(PKG)/internal/version.Date=$(DATE)

.PHONY: build install test vet fmt lint clean tidy

build: ## Build the binary into ./bin
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

install: ## Install into $GOBIN
	go install -ldflags "$(LDFLAGS)" .

test: ## Run tests
	go test ./...

vet: ## go vet
	go vet ./...

fmt: ## gofmt
	gofmt -w .

lint: ## golangci-lint (if installed)
	golangci-lint run ./... || echo "golangci-lint not installed; skipping"

tidy: ## Tidy modules
	go mod tidy

clean:
	rm -rf bin dist
