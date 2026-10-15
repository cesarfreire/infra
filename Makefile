BIN := bin/infra
GOLANGCI_LINT ?= $(shell command -v golangci-lint || echo $(HOME)/go/bin/golangci-lint)

.PHONY: build run test cover lint fmt tidy release-check snapshot

build:
	go build -o $(BIN) ./cmd/infra

run:
	go run ./cmd/infra $(ARGS)

test:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

lint:
	$(GOLANGCI_LINT) run

fmt:
	$(GOLANGCI_LINT) fmt

tidy:
	go mod tidy

release-check:
	goreleaser check

# Builds every release archive locally into dist/, without publishing.
snapshot:
	goreleaser release --snapshot --clean
