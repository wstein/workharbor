# Tool versions are pinned; tools run via `go run`, so nothing needs installing.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
# Hugo extended builds with cgo, so a C++ compiler is needed the first time.
HUGO := go run -tags extended github.com/gohugoio/hugo@v0.167.0
EDITORCONFIG_CHECKER := github.com/editorconfig-checker/editorconfig-checker/v3/cmd/editorconfig-checker@v3.11.3
GITLEAKS := github.com/zricethezav/gitleaks/v8@v8.30.1

.DEFAULT_GOAL := build

.PHONY: generate check-generated release-prep release-snapshot build install install-release check-clean check-main test vet fmt fmt-check lint editorconfig check commitlint changelog docs docs-serve hooks check-ci check-hooks secrets-staged secrets-range land

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

# make install builds whr, the launcher whr-shim and the egress proxy whr-proxy
# (both linux-arm64: the tool store and the sidecar) from the current commit,
# with the version stamp, and installs them under PREFIX. It refuses a dirty
# tree and a commit that is not on origin/main, so the supervisor always runs
# approved, committed code (D34), and builds with GOWORK=off and no GOFLAGS, so
# a parent go.work or the environment cannot change what is built. The whr user
# runs it with PREFIX=$$HOME/.local, or any PREFIX it can write.
PREFIX ?= $(HOME)/.local
INSTALL_GO = GOWORK=off GOFLAGS= go

check-clean:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "refusing to install from a dirty tree: commit or stash first, so that whr runs committed code" >&2; \
		git status --short >&2; exit 1; \
	fi

check-main:
	@if ! git merge-base --is-ancestor HEAD origin/main 2>/dev/null; then \
		echo "refusing to install $$(git rev-parse --short HEAD): it is not on origin/main (run git fetch origin, or install a merged commit)" >&2; \
		exit 1; \
	fi

install: check-clean check-main
	mkdir -p $(PREFIX)/bin $(PREFIX)/libexec/whr
	$(INSTALL_GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(PREFIX)/bin/whr ./cmd/whr
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(INSTALL_GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(PREFIX)/libexec/whr/whr-shim-linux-arm64 ./cmd/whr-shim
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(INSTALL_GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(PREFIX)/libexec/whr/whr-proxy-linux-arm64 ./cmd/whr-proxy
	@echo "installed whr $$($(PREFIX)/bin/whr version), whr-shim and whr-proxy (linux-arm64) under $(PREFIX)"
	@echo "next: $(PREFIX)/bin/whr tools build -store <tool store> -shim $(PREFIX)/libexec/whr/whr-shim-linux-arm64"

# Install a release, a dogfood draft included (D24, D34), as the administrator
# into a prefix whr cannot write: make install-release VERSION=v0.1.0-alpha.1
# [PREFIX=/opt/whr]. It checks the checksums and the provenance attestation.
install-release:
	@test -n "$(VERSION)" || { echo "usage: make install-release VERSION=<tag> [PREFIX=/opt/whr]" >&2; exit 2; }
	scripts/install-release.sh "$(VERSION)" "$(if $(filter command line,$(origin PREFIX)),$(PREFIX),/opt/whr)"

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

# Prepare a release (design D24): regenerate CHANGELOG.md for VERSION and commit
# it as chore(release). It does not tag; the human tags, signed, after CI passes.
release-prep: check-clean
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || { echo "usage: make release-prep VERSION=vX.Y.Z" >&2; exit 1; }
	npx --yes git-cliff@2.14.2 --tag "$(VERSION)" --output CHANGELOG.md
	git add CHANGELOG.md
	git commit -m "chore(release): prepare $(VERSION)"

# Build the release artifacts locally into dist/ without publishing anything.
# The SBOM needs a tag, so it is skipped here; the snapshot workflow tags the
# runner's copy and builds it too.
release-snapshot:
	go run github.com/goreleaser/goreleaser/v2@v2.18.2 release --snapshot --clean --skip=publish,sbom

# Run what CI runs beyond make check, before a branch is merged or rebased into
# main: the docs build, spelling (typos), links (lychee, online, as CI does),
# secrets (gitleaks over the history being merged, as CI scans it) and the
# workflows (actionlint). typos and lychee come from Homebrew
# (brew install typos-cli lychee); the rest run through pinned `go run`.
TYPOS_VERSION := 1.50.3
check-ci: docs check-hooks check-generated
	@command -v typos >/dev/null || { echo "typos is missing: brew install typos-cli (CI pins $(TYPOS_VERSION))" >&2; exit 1; }
	@command -v lychee >/dev/null || { echo "lychee is missing: brew install lychee" >&2; exit 1; }
	typos --config typos.toml .
	lychee --config lychee.toml --no-progress '*.md' 'docs/content/**/*.md' 'design/**/*.md'
	go run $(GITLEAKS) git --no-banner --redact --config .gitleaks.toml --log-opts=HEAD .
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7

# The web UI's templates (internal/web/*.templ, D8) are compiled to Go by templ,
# pinned here; the generated files are committed. check-generated fails when a
# template was changed without regenerating.
TEMPL := github.com/a-h/templ/cmd/templ@v0.3.1020
generate:
	go run $(TEMPL) generate -path internal/web

check-generated: generate
	@git diff --exit-code -- 'internal/web/*_templ.go' >/dev/null || { echo "the generated templates are stale: run make generate and commit them" >&2; exit 1; }

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

# Fail unless this clone runs the repository's hooks, which scan for secrets
# before a commit and before a push: a session that never ran `make hooks`
# would commit and merge unchecked.
check-hooks:
	@if [ "$$(git config core.hooksPath)" != ".githooks" ]; then \
		echo "the repository's hooks are not enabled in this clone: run make hooks" >&2; exit 1; \
	fi

# Land the current branch on main, from a session's own worktree: refuse unless
# the shared checkout is on main (a detached HEAD there once swallowed merges),
# the branch is rebased onto main, and check, check-ci and commitlint pass; then
# fast-forward main, unless main moved during the checks (rebase and run again).
land:
	@shared="$$(dirname "$$(git rev-parse --path-format=absolute --git-common-dir)")"; \
	branch="$$(git symbolic-ref -q --short HEAD)" || { echo "land: check out the branch to land first" >&2; exit 1; }; \
	if [ "$$branch" = main ]; then echo "land: run it on a topic branch in your own worktree, not on main" >&2; exit 1; fi; \
	if [ "$$(git -C "$$shared" symbolic-ref -q HEAD)" != refs/heads/main ]; then \
		echo "land: the shared checkout $$shared is not on main: stop and tell the human (never switch it yourself)" >&2; exit 1; fi; \
	base="$$(git rev-parse main)"; \
	git merge-base --is-ancestor "$$base" HEAD || { echo "land: $$branch is not on top of main: git rebase main first" >&2; exit 1; }; \
	$(MAKE) -s check check-ci commitlint || exit 1; \
	if [ "$$(git rev-parse main)" != "$$base" ]; then echo "land: main moved during the checks: git rebase main and run make land again" >&2; exit 1; fi; \
	if [ "$$(git -C "$$shared" symbolic-ref -q HEAD)" != refs/heads/main ]; then echo "land: the shared checkout left main during the checks: stop and tell the human" >&2; exit 1; fi; \
	git -C "$$shared" merge -q --ff-only "$$branch" && echo "land: main is now $$(git rev-parse --short main)"

# Scan the commits of a git log range for secrets: the pre-push hook runs it
# with the range about to be pushed.
secrets-range:
	@test -n "$(RANGE)" || { echo "secrets-range needs RANGE" >&2; exit 2; }
	go run $(GITLEAKS) git --no-banner --redact --config .gitleaks.toml --log-opts="$(RANGE)" .

# Scan what is staged for secrets: the pre-commit hook runs it.
secrets-staged:
	@go run $(GITLEAKS) git --pre-commit --staged --no-banner --redact --config .gitleaks.toml . >/dev/null 2>&1 || { \
		echo "a secret is staged: unstage it, revoke it if it is real, and read it from a 0600 env file instead (AGENTS.md, Secrets)" >&2; \
		go run $(GITLEAKS) git --pre-commit --staged --no-banner --redact --config .gitleaks.toml . >&2; exit 1; }
