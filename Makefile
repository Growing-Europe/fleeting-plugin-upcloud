# fleeting-plugin-upcloud — developer convenience targets.
# CI is the source of truth (.github/workflows); these mirror it for local use.

GO        ?= go
BINARY    ?= fleeting-plugin-upcloud
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -X main.Version=$(VERSION)

.PHONY: all build vet test test-race cover fmt fmt-check lint vuln gosec gitleaks verify tidy clean tools

all: verify

build:
	$(GO) build -ldflags "$(LDFLAGS)" ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race -covermode=atomic -coverprofile=coverage.out ./...

cover: test-race
	$(GO) tool cover -func=coverage.out

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

lint:
	golangci-lint run

vuln:
	govulncheck ./...

gosec:
	gosec -exclude-generated ./...

gitleaks:
	gitleaks detect --source . --redact --no-banner --verbose

# Full pre-commit gate — mirrors AGENTS.md "Build / verify".
verify: build vet test-race fmt-check
	@echo "verify: OK"

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BINARY) coverage.out
	rm -rf dist/

# Install the local-dev linters/scanners (CI installs pinned versions itself).
tools:
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	$(GO) install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@v2.27.1
	$(GO) install github.com/gitleaks/gitleaks/v8@v8.24.3
