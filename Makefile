BIN     := recon
PKG     := ./cmd/recon
TOOLS   := $(CURDIR)/.tools/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

GOLANGCI_LINT_VERSION := v1.63.4

.PHONY: all build test cover bench lint fmt vet tidy demo tools clean

all: fmt vet test build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) $(PKG)

test:
	go test -race -cover ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

bench:
	go test -run '^$$' -bench . -benchmem ./internal/match/

lint: tools
	$(TOOLS)/golangci-lint run ./...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

tidy:
	go mod tidy

# Reconcile the bundled sample files. Exit code 2 is expected: the samples
# deliberately contain exceptions.
demo: build
	-./bin/$(BIN) run --auths testdata/auths.csv --clearing testdata/clearing.csv

tools:
	@test -x $(TOOLS)/golangci-lint || GOBIN=$(TOOLS) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

clean:
	rm -rf bin dist coverage.out
