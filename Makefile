# Tool versions are pinned; tools run via `go run`, so nothing needs installing.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
# Hugo extended builds with cgo, so a C++ compiler is needed the first time.
HUGO := go run -tags extended github.com/gohugoio/hugo@v0.167.0
EDITORCONFIG_CHECKER := github.com/editorconfig-checker/editorconfig-checker/v3/cmd/editorconfig-checker@v3.11.3

.PHONY: build install check-clean test vet fmt fmt-check lint editorconfig check commitlint changelog docs docs-serve hooks

# The version comes from the tag (design §13): git describe, or v0.0.0-<commits>-g<sha>
# when there is no tag, never empty. The tree is dirty if anything is uncommitted.
BIN ?= bin/whr
VERSION_PKG := github.com/wstein/workharbor/internal/version
GIT_COMMIT = $(shell git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
GIT_VERSION = $(shell git describe --tags --match 'v[0-9]*' --abbrev=7 2>/dev/null | sed 's/-dirty$$//' || true)
GIT_DIRTY = $(shell if [ -n "$$(git status --porcelain 2>/dev/null)" ]; then echo true; else echo false; fi)
BUILD_VERSION = $(or $(GIT_VERSION),v0.0.0-$(shell git rev-list --count HEAD 2>/dev/null || echo 0)-g$(GIT_COMMIT))
BUILD_DATE = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -X $(VERSION_PKG).Version=$(BUILD_VERSION) -X $(VERSION_PKG).Commit=$(GIT_COMMIT) -X $(VERSION_PKG).Dirty=$(GIT_DIRTY) -X $(VERSION_PKG).Date=$(BUILD_DATE)

# make install builds whr and the launcher whr-shim (linux-arm64, for the tool
# store) from the current commit, with the version stamp, and installs them under
# PREFIX. It refuses a dirty tree, so the supervisor always runs committed code
# (D34). The whr user runs it with PREFIX=$$HOME/.local, or any PREFIX it can write.
PREFIX ?= $(HOME)/.local

check-clean:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "refusing to install from a dirty tree: commit or stash first, so that whr runs committed code" >&2; \
		git status --short >&2; exit 1; \
	fi

install: check-clean
	mkdir -p $(PREFIX)/bin $(PREFIX)/libexec/whr
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(PREFIX)/bin/whr ./cmd/whr
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(PREFIX)/libexec/whr/whr-shim-linux-arm64 ./cmd/whr-shim
	@echo "installed whr $$($(PREFIX)/bin/whr version) and whr-shim (linux-arm64) under $(PREFIX)"
	@echo "next: $(PREFIX)/bin/whr tools build -store <tool store> -shim $(PREFIX)/libexec/whr/whr-shim-linux-arm64"

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/whr

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

# Check commits on this branch that are not on origin/main.
commitlint:
	go run ./cmd/commitlint --range origin/main..HEAD

# Regenerate CHANGELOG.md from Conventional Commits (git-cliff via npx).
changelog:
	npx --yes git-cliff@2 --output CHANGELOG.md

# Build the documentation site into _site (Hugo, pinned; fetches the Hextra module).
docs:
	cd docs && $(HUGO) --gc --minify --panicOnWarning --destination ../_site

# Serve the documentation site locally with live reload.
docs-serve:
	cd docs && $(HUGO) server

# Enable the repository git hooks and the commit message template.
hooks:
	git config core.hooksPath .githooks
	git config commit.template .gitmessage
