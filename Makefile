# Tool versions are pinned; tools run via `go run`, so nothing needs installing.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
EDITORCONFIG_CHECKER := github.com/editorconfig-checker/editorconfig-checker/v3/cmd/editorconfig-checker@v3.11.3

.PHONY: build test vet fmt fmt-check lint editorconfig check hooks

build:
	go build -o bin/whr ./cmd/whr

test:
	go test ./...

vet:
	go vet ./...

# Rewrite sources with gofumpt and goimports.
fmt:
	go run $(GOLANGCI_LINT) fmt

# Fail if any source is not formatted.
fmt-check:
	@diff="$$(go run $(GOLANGCI_LINT) fmt --diff)"; \
	if [ -n "$$diff" ]; then echo "$$diff"; echo "run 'make fmt'"; exit 1; fi

lint:
	go run $(GOLANGCI_LINT) run

# Enforce .editorconfig on all tracked files.
editorconfig:
	go run $(EDITORCONFIG_CHECKER)

check: fmt-check vet lint editorconfig test

# Enable the repository git hooks (pre-commit, commit-msg).
hooks:
	git config core.hooksPath .githooks
