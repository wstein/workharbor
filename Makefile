# Tool versions are pinned; tools run via `go run`, so nothing needs installing.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
# Hugo extended builds with cgo, so a C++ compiler is needed the first time.
HUGO := go run -tags extended github.com/gohugoio/hugo@v0.165.0
EDITORCONFIG_CHECKER := github.com/editorconfig-checker/editorconfig-checker/v3/cmd/editorconfig-checker@v3.11.3
GITLEAKS := github.com/zricethezav/gitleaks/v8@v8.30.1
# The secret scans go install gitleaks first (a failed install means the scan could not
# run) and run the binary with this exit code for a finding: `go run` turns every
# non-zero exit into 1, and gitleaks itself exits 1 on any fatal error. Nothing
# in the environment replaces the scanner; a test puts a fake go on the PATH.
GITLEAKS_FOUND := 42

.DEFAULT_GOAL := build

.PHONY: generate check-generated release-prep release-snapshot build install install-release check-clean check-main test test-short race vet fmt fmt-check lint editorconfig check check-local commitlint changelog docs docs-build docs-schema docs-serve hooks check-ci check-hooks secrets-staged fuzz secrets-range test-commitlint-consumers land land-list land-next land-all land-preview temp-ls temp-clean

# The version comes from the tag (design §13): git describe, or v0.0.0-<commits>-g<sha>
# when there is no tag, never empty. The tree is dirty if anything is uncommitted.
BIN ?= bin/whr
VERSION_PKG := github.com/wstein/workharbor/internal/version
VERSION_GIT = git
GIT_COMMIT = $(shell $(VERSION_GIT) rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
GIT_VERSION = $(shell $(VERSION_GIT) describe --tags --match 'v[0-9]*' --abbrev=7 2>/dev/null | sed 's/-dirty$$//' || true)
GIT_STATUS_FLAGS = --porcelain
GIT_DIRTY = $(shell if [ -n "$$($(VERSION_GIT) status $(GIT_STATUS_FLAGS) 2>/dev/null)" ]; then echo true; else echo false; fi)
BUILD_VERSION = $(or $(GIT_VERSION),v0.0.0-$(shell $(VERSION_GIT) rev-list --count HEAD 2>/dev/null || echo 0)-g$(GIT_COMMIT))
BUILD_DATE = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -X $(VERSION_PKG).Version=$(BUILD_VERSION) -X $(VERSION_PKG).Commit=$(GIT_COMMIT) -X $(VERSION_PKG).Dirty=$(GIT_DIRTY) -X $(VERSION_PKG).Date=$(BUILD_DATE)

# make install builds whr, the launcher whr-shim and the egress proxy whr-proxy
# (both linux-arm64: the tool store and the sidecar) from the current commit,
# with the version stamp, and installs them under PREFIX. It refuses a dirty
# tree and HEAD differing from current local main. The operator obtains
# independent review of that exact commit (D24, D34); Git equality cannot prove
# approval. Unpublished main is accepted only for a development installation.
# It builds with GOWORK=off and no GOFLAGS, so
# a parent go.work or the environment cannot change what is built. The whr user
# runs it with an existing user-owned PREFIX=$$HOME/.local, or an existing safe
# development PREFIX outside Git checkouts and managed locations. It removes
# libexec/whr/VERSION, which only install-release writes: after a source install
# the version is unknown, and install-release then needs --allow-downgrade.
PREFIX ?= $(HOME)/.local
INSTALL_GO = GOWORK=off GOFLAGS= go
# Quote operator-selected paths as shell data, including spaces and apostrophes.
install-quote = '$(subst ','"'"',$(1))'

check-clean:
	@s=$$(git status --porcelain) || { echo "could not read the git status, so the tree is not known to be clean" >&2; exit 1; }; \
	if [ -n "$$s" ]; then \
		echo "refusing to install from a dirty tree: commit or stash first, so that whr runs committed code" >&2; \
		echo "$$s" >&2; exit 1; \
	fi

check-main:
	@if ! git merge-base --is-ancestor HEAD origin/main 2>/dev/null; then \
		echo "refusing to install $$(git rev-parse --short HEAD): it is not on origin/main (run git fetch origin, or install a merged commit)" >&2; \
		exit 1; \
	fi

.PHONY: check-install-source
check-install-source:
	@$(INSTALL_GO) run scripts/install-source.go $(call install-quote,$(PREFIX))

# Stamp the guarded checkout even when the caller has inherited Git selectors
# or configuration. Ordinary make build retains its existing behavior.
install: VERSION_GIT = env -i PATH=$(call install-quote,$(PATH)) HOME=$(call install-quote,$(HOME)) GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_GLOBAL=/dev/null GIT_OPTIONAL_LOCKS=0 GIT_NO_REPLACE_OBJECTS=1 git -c credential.helper= -c core.fsmonitor=false
install: GIT_STATUS_FLAGS = --porcelain --untracked-files=all
install: check-install-source
	mkdir -p $(call install-quote,$(PREFIX)/bin) $(call install-quote,$(PREFIX)/libexec/whr)
	rm -f $(call install-quote,$(PREFIX)/libexec/whr/VERSION)
	set -e; \
	bin=$(call install-quote,$(PREFIX)/bin); lib=$(call install-quote,$(PREFIX)/libexec/whr); \
	tmp=$$(mktemp -d "$$bin/.whr-install.XXXXXX"); tmplib=$$(mktemp -d "$$lib/.whr-install.XXXXXX"); \
	trap 'rm -rf "$$tmp" "$$tmplib"' EXIT; \
	$(INSTALL_GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$$tmp/whr" ./cmd/whr; \
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(INSTALL_GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$$tmplib/whr-shim-linux-arm64" ./cmd/whr-shim; \
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(INSTALL_GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$$tmplib/whr-proxy-linux-arm64" ./cmd/whr-proxy; \
	if [ "$$(uname -s)" = Darwin ]; then codesign --force --sign - "$$tmp/whr"; fi; \
	mv -f "$$tmp/whr" "$$bin/whr"; \
	mv -f "$$tmplib/whr-shim-linux-arm64" "$$lib/whr-shim-linux-arm64"; \
	mv -f "$$tmplib/whr-proxy-linux-arm64" "$$lib/whr-proxy-linux-arm64"
	@version=$$($(call install-quote,$(PREFIX)/bin/whr) version) || { echo "the installed whr did not run (zsh shows 'killed' with no output when macOS rejects its signature): run codesign -v and xattr -l on $(PREFIX)/bin/whr, see the troubleshooting page, then rebuild with make install" >&2; exit 1; }; printf 'installed whr %s, whr-shim and whr-proxy (linux-arm64) under %s\n' "$$version" $(call install-quote,$(PREFIX))
	@printf '%s\n' $(call install-quote,development setup: $(PREFIX)/bin/whr setup --dev --prefix $(PREFIX) --user <your-account> (user-writable supervisor; see the installation manual))
	@printf '%s\n' $(call install-quote,next: $(PREFIX)/bin/whr tools build -store <tool store> -shim $(PREFIX)/libexec/whr/whr-shim-linux-arm64)

# Install a release, a dogfood draft included (D24, D34), as the administrator
# into a prefix whr cannot write: make install-release VERSION=v0.1.0-alpha.1
# (an older tag than the installed one needs ALLOW_DOWNGRADE=1)
# [PREFIX=/opt/whr]. It checks the checksums and the provenance attestation.
install-release:
	@test -n "$(VERSION)" || { echo "usage: make install-release VERSION=<tag> [PREFIX=/opt/whr]" >&2; exit 2; }
	scripts/install-release.sh "$(VERSION)" "$(if $(filter command line,$(origin PREFIX)),$(PREFIX),/opt/whr)" $(if $(ALLOW_DOWNGRADE),--allow-downgrade)

# Temporary Apple Container resources of spikes, live tests and debugging
# (AGENTS.md): list them, or remove one lane's (LANE=wh/spikes); ALL=1 removes
# every lane's, which can stop another session's running test.
temp-ls:
	scripts/temp-resources.sh $(if $(LANE),--lane $(LANE))

temp-clean:
	@test -n "$(LANE)$(ALL)" || { echo "usage: make temp-clean LANE=<lane> (ALL=1 removes every lane's, including other sessions' running tests)" >&2; exit 2; }
	scripts/temp-resources.sh $(if $(LANE),--lane $(LANE)) --delete

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/whr

test:
	go test ./...

# The inner loop: skips the slowest tests (each guarded by testing.Short). make check,
# make land and CI run the whole suite.
test-short:
	go test -short ./...

# The race detector, on the packages that run goroutines of their own: the
# service (starts, sessions, the reconciler), the API (streams), the store, the
# runtime adapters, the agent adapters and approval broker, the egress proxy,
# the preview listeners, `whr serve` and the in-guest shim. About a minute.
race:
	go test -race ./internal/service ./internal/api ./internal/store ./internal/runtime/... \
		./internal/agent/... ./internal/egress ./internal/preview ./internal/serve ./cmd/whr-shim

# Native Go fuzz targets for every parser of untrusted input (issue #110): each runs
# for FUZZTIME (go's -fuzztime). A crash writes its input under the package's
# testdata/fuzz, which is then a regression test: commit it with the fix. The
# scheduled workflow runs this; it is not part of make check.
FUZZTIME ?= 30s
export FUZZTIME
FUZZ_TARGETS = \
	./internal/redact:FuzzRedact \
	./internal/commitlint:FuzzLint \
	./internal/devcontainer:FuzzParse \
	./internal/devcontainer:FuzzExport \
	./internal/agent/claude:FuzzParseStream \
	./internal/agent/claude:FuzzControlRequests \
	./internal/service:FuzzParseIssueURL \
	./internal/service:FuzzIssuePrompt \
	./internal/forge/github:FuzzGitHubAnswers \
	./internal/forge/github:FuzzVerifyWebhook \
	./internal/oci:FuzzExtract \
	./internal/oci:FuzzParseRef
# FUZZTIME reaches the shell as an environment variable, never spliced into the
# command, and must look like a go duration or an exec count.
fuzz:
	@printf '%s' "$$FUZZTIME" | grep -Eq '^[0-9]+(s|m|h|x)$$' || { echo "FUZZTIME must be like 30s, 2m, 1h or 5000x" >&2; exit 2; }
	@set -e; for t in $(FUZZ_TARGETS); do pkg=$${t%%:*}; name=$${t##*:}; \
		echo "fuzz $$name ($$pkg) for $$FUZZTIME"; \
		go test "$$pkg" -run '^$$' -fuzz "^$$name\$$" -fuzztime "$$FUZZTIME"; done

vet:
	go vet ./...

# Rewrite sources with gofumpt and goimports.
fmt:
	go run $(GOLANGCI_LINT) fmt

# Fail if any source is not formatted.
fmt-check:
	@d=$$(mktemp -d) || { echo "the format check could not run (no temporary directory), so nothing was checked" >&2; exit 1; }; \
	trap 'rm -rf "$$d"' EXIT; trap 'exit 1' HUP INT TERM; \
	go run $(GOLANGCI_LINT) fmt --diff >"$$d/out" 2>"$$d/err"; rc=$$?; \
	n=$$(sed -n 's/^exit status \([0-9][0-9]*\)$$/\1/p' "$$d/err" | tail -1); [ -n "$$n" ] || n=$$rc; \
	grep -Ev '^(exit status [0-9]+|go: (downloading|finding|extracting) .*)$$' "$$d/err" >"$$d/real"; \
	if [ -s "$$d/out" ]; then cat "$$d/out"; [ -s "$$d/real" ] && cat "$$d/real" >&2; \
		{ [ -s "$$d/real" ] || [ "$$n" -ne 1 ]; } && echo "the formatter also failed (exit $$n), so the diff above may be partial" >&2; \
		echo "run 'make fmt'" >&2; exit 1; fi; \
	if [ $$rc -ne 0 ]; then cat "$$d/real" >&2; echo "the formatter failed (exit $$n), so nothing was checked" >&2; exit 1; fi

lint:
	go run $(GOLANGCI_LINT) run --config .config/golangci.yml

# Enforce .editorconfig on all tracked files.
editorconfig:
	go run $(EDITORCONFIG_CHECKER)

check: fmt-check vet lint editorconfig test race

# Local mechanical gates; focused behaviour/regression evidence is reviewed
# separately for the exact candidate. Full CI aggregates remain unchanged.
check-local: fmt-check lint editorconfig check-hooks

# Check commits on this branch that are not on origin/main.
commitlint:
	go run ./cmd/commitlint --range origin/main..HEAD

# Regenerate CHANGELOG.md from Conventional Commits (git-cliff via npx).
changelog:
	npx --yes git-cliff@2 --config .config/cliff.toml --output CHANGELOG.md

# Prepare a release (design D24): regenerate CHANGELOG.md for VERSION and commit
# it as chore(release). It does not tag; the human tags, signed, after CI passes.
release-prep: check-clean
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || { echo "usage: make release-prep VERSION=vX.Y.Z" >&2; exit 1; }
	npx --yes git-cliff@2.14.2 --config .config/cliff.toml --tag "$(VERSION)" --output CHANGELOG.md
	git add CHANGELOG.md
	git commit -m "chore(release): prepare $(VERSION)"

# Build the release artifacts locally into dist/ without publishing anything.
# The SBOM needs a tag, so it is skipped here; the snapshot workflow tags the
# runner's copy and builds it too.
release-snapshot:
	go run github.com/goreleaser/goreleaser/v2@v2.18.2 release --config .config/goreleaser.yaml --snapshot --clean --skip=publish,sbom

# Run what CI runs beyond make check; local full suites require an explicit
# human request before push: the docs build, spelling (typos), links (lychee, online, as CI does;
# links into this repository's main are checked against the local files, so
# a file moved on local main does not fail before the push),
# secrets (gitleaks over the history being merged, as CI scans it) and the
# workflows (actionlint). typos and lychee come from Homebrew
# (brew install typos-cli lychee); the rest run through pinned `go run`.
TYPOS_VERSION := 1.50.3
check-ci: docs check-hooks check-generated
	@command -v typos >/dev/null || { echo "typos is missing: brew install typos-cli (CI pins $(TYPOS_VERSION))" >&2; exit 1; }
	@command -v lychee >/dev/null || { echo "lychee is missing: brew install lychee" >&2; exit 1; }
	typos --config .config/typos.toml .
	lychee --config .config/lychee.toml --no-progress \
		--remap 'https://github\.com/wstein/workharbor/(?:blob|tree)/main/([^?#]*)(?:[?#].*)? file://$(CURDIR)/$$1' \
		'*.md' '.github/*.md' 'docs/content/**/*.md' 'design/**/*.md'
	go run $(GITLEAKS) git --no-banner --redact --config .gitleaks.toml --log-opts=HEAD .
	@m=$$(mktemp) && trap 'rm -f "$$m"' EXIT && scripts/messages.sh "" HEAD >"$$m" && go run $(GITLEAKS) stdin --no-banner --redact --config .gitleaks.toml <"$$m"
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7

# The web UI's templates (internal/web/*.templ, D8) are compiled to Go by templ,
# pinned here; the generated files are committed. check-generated fails when a
# template was changed without regenerating.
TEMPL := github.com/a-h/templ/cmd/templ@v0.3.1020
generate:
	go run $(TEMPL) generate -path internal/web

check-generated: generate
	@git diff --exit-code -- 'internal/web/*_templ.go' >/dev/null || { echo "the generated templates are stale: run make generate and commit them" >&2; exit 1; }

# Build the documentation and check the published canonical API contract.
DOCS_DEST ?= $(CURDIR)/_site
docs: docs-build
	$(MAKE) docs-schema

docs-build:
	cd docs && $(HUGO) --gc --minify --panicOnWarning --destination "$(DOCS_DEST)"

docs-schema:
	cd scripts && go test docs_schema_test.go -count=1 -args -docs-site="$(DOCS_DEST)"

# Serve the documentation site locally with live reload.
docs-serve:
	cd docs && $(HUGO) server

# Enable the repository git hooks and the commit message template.
hooks:
	git config core.hooksPath .githooks
	git config commit.template .config/gitmessage

# Fail unless this clone runs the repository's hooks, which scan for secrets
# before a commit and before a push: a session that never ran `make hooks`
# would commit and merge unchecked.
check-hooks:
	@if [ "$$(git config core.hooksPath)" != ".githooks" ]; then \
		echo "the repository's hooks are not enabled in this clone: run make hooks" >&2; exit 1; \
	fi

# The packages that consume internal/commitlint (the commit-msg hook, hostgit's
# commit checks, the serve and service fixtures that build commits). check-local
# runs no tests, so a commitlint change that breaks a consumer reached CI (#329).
# make land runs this on every landing, not only when internal/commitlint changes:
# the consumers also break through their own changes, and a path trigger is one more
# thing to get wrong; the timeout bounds a hang. GOENV=off GOFLAGS= pins the go
# environment: a caller's GOFLAGS (-run=NONE, -exec=true, -skip) or a go env -w
# file must not turn the gate into a pass.
CONSUMER_PKGS := ./internal/commitlint ./cmd/commitlint ./internal/hostgit ./internal/serve ./internal/service
CONSUMER_TEST_TIMEOUT := 300s
test-commitlint-consumers:
	GOENV=off GOFLAGS= go test -count=1 -timeout $(CONSUMER_TEST_TIMEOUT) $(CONSUMER_PKGS)

# The sub-makes of land go through LAND_MAKE: the recipe line must not contain
# $(MAKE) itself, or make -n, -t and -q would run it for real (merge included).
override LAND_MAKE := $(MAKE)
# They also run without the caller's MAKEFLAGS: -i or a command-line override such
# as GITLEAKS_FOUND=0 would otherwise turn a failing check into a pass. Both are
# override variables so that no command-line or -e setting replaces them.
# An absolute executable also prevents an exported env shell function from
# swallowing the sub-makes. This is narrow hardening, not a shell sandbox.
override LAND_CLEAN := /usr/bin/env -u MAKEFLAGS -u MFLAGS -u GNUMAKEFLAGS

# Queue order is lexical branch name; preview/list never confirm or land.
# Decisions always use main's resolver, just like land SHA=.
# Refuse a caller's SHELL while expanding the recipe, before that shell can
# suppress execution with -n. GNU make treats its built-in /bin/sh as file
# origin when it ignores a normal inherited SHELL; that case remains supported.
land-list land-next land-all land-preview:
	$(if $(filter default file,$(origin SHELL)),,$(error land: SHELL is set by the caller: refusing))
	@if [ "$(origin MAKE)" != default ] || [ "$(origin MAKE_COMMAND)" != default ] || [ -n '$(subst ','\'',$(MAKEFILES))' ]; then echo "land: MAKE or MAKEFILES is set by the caller: refusing" >&2; exit 1; fi; \
	lsh="$$(git --no-replace-objects show refs/heads/main:scripts/land.sh)" || exit 1; \
	case "$@" in land-preview) [ "$(origin SHA)" = "command line" ] || { echo "usage: make land-preview SHA=<sha>" >&2; exit 1; }; sh -c "$$lsh" land.sh preview "$$SHA";; \
	*) sh -c "$$lsh" land.sh "$(patsubst land-%,%,$@)";; esac

# No arguments starts the human wizard using main's resolver. Explicit BRANCH=<name>
# lands a branch from a session's own worktree (BRANCH=<name>
# from any checkout: land the worktree that has it checked out; SHA=<full sha>:
# refuse unless the candidate is that commit; see the manual): refuse unless
# the shared checkout is on main (a detached HEAD there once swallowed merges),
# the branch is rebased onto main, and local checks and candidate scans pass; then
# fast-forward main, unless main moved during the checks (rebase and run again).
land:
	$(if $(filter default file,$(origin SHELL)),,$(error land: SHELL is set by the caller: refusing))
	@if [ "$(origin MAKE)" != default ] || [ "$(origin MAKE_COMMAND)" != default ] || [ -n '$(subst ','\'',$(MAKEFILES))' ]; then echo "land: MAKE or MAKEFILES is set by the caller: refusing" >&2; exit 1; fi; \
	if [ "$(origin SHA)" != "command line" ] && [ "$(origin BRANCH)" != "command line" ]; then \
		lsh="$$(git --no-replace-objects show refs/heads/main:scripts/land.sh)" || { echo "land: cannot read main resolver: refusing" >&2; exit 1; }; \
		sh -c "$$lsh" land.sh wizard; exit $$?; fi; \
	want=""; wb=""; \
	short=""; det=""; tmp=""; \
	if [ "$(origin SHA)" = "command line" ]; then \
		case "$$SHA" in ""|*[!0-9a-f]*) echo "land: SHA must be 7 to 40 lowercase hex characters (the full 40-character id with BRANCH=)" >&2; exit 1;; esac; \
		if [ "$${#SHA}" -lt 7 ] || [ "$${#SHA}" -gt 40 ]; then echo "land: SHA must be 7 to 40 lowercase hex characters (the full 40-character id with BRANCH=)" >&2; exit 1; fi; \
		if [ "$${#SHA}" != 40 ] && [ "$(origin BRANCH)" = "command line" ]; then echo "land: with BRANCH= the SHA must be the full 40-character lowercase hex commit id" >&2; exit 1; fi; \
		if [ "$(origin BRANCH)" != "command line" ]; then \
			lsh="$$(git --no-replace-objects show refs/heads/main:scripts/land.sh)" || { echo "land: main has no scripts/land.sh: refusing" >&2; exit 1; }; \
			res="$$(sh -c "$$lsh" land.sh resolve "$$SHA")" || exit 1; \
			set -f; set -- $$res; set +f; \
			[ "$$#" = 8 ] || { echo "land: invalid resolver confirmation: refusing" >&2; exit 1; }; \
			short=1; want="$$1"; BRANCH="$$2"; confirm_mode="$$3"; confirm_answer="$$4"; confirm_at="$$5"; confirm_review="$$6"; confirm_base="$$7"; confirm_class="$$8"; \
		else want="$$SHA"; fi; fi; \
	if [ "$(origin BRANCH)" = "command line" ] || [ -n "$$short" ]; then \
		wb="$$BRANCH"; \
		case "$$wb" in "") echo "land: BRANCH is empty" >&2; exit 1;; main) echo "land: BRANCH=main: never land main" >&2; exit 1;; -*) echo "land: BRANCH must not start with a dash" >&2; exit 1;; esac; \
		git check-ref-format "refs/heads/$$wb" || { echo "land: BRANCH is not a valid branch name (give the short name, not refs/heads/...)" >&2; exit 1; }; \
		if [ "$$(git rev-parse --is-bare-repository)" != false ]; then echo "land: run it from a checkout, not a bare repository" >&2; exit 1; fi; \
		found="$$(git worktree list --porcelain -z | tr '\n\0' '\001\n' | awk -v ref="refs/heads/$$wb" 'function flush() { if (isbr) print (prun ? "P " : "W ") cur; isbr = 0; prun = 0 } /^worktree / { cur = substr($$0, 10) } $$0 == "branch " ref { isbr = 1 } /^prunable/ { prun = 1 } /^$$/ { flush() } END { flush() }')" || { echo "land: cannot list the worktrees" >&2; exit 1; }; \
		if [ -z "$$found" ] && [ -z "$$short" ]; then echo "land: no worktree has $$wb checked out: check it out in a worktree first (never in the shared checkout)" >&2; exit 1; fi; \
		if [ -z "$$found" ]; then \
			det=1; sc="$$(dirname "$$(git rev-parse --path-format=absolute --git-common-dir)")"; \
			tmp="$$(mktemp -d "$${TMPDIR:-/tmp}/land.XXXXXX")" || { echo "land: cannot create a temporary directory" >&2; exit 1; }; \
			tmpc="$$(cd -P "$$tmp" && pwd -P)" || exit 1; \
			trap 'cd /; git -C "$$sc" worktree remove --force "$$tmp/wt" >/dev/null 2>&1; rm -rf "$$tmp"; if [ -e "$$tmp" ] || git -C "$$sc" worktree list --porcelain | grep -qxF "worktree $$tmpc/wt"; then echo "land: could not remove the temporary worktree $$tmp/wt: stop and tell the human" >&2; exit 1; fi' EXIT; \
			trap 'exit 130' INT; trap 'exit 143' TERM; trap 'exit 129' HUP; \
			git worktree add -q --detach "$$tmp/wt" "$$want" || { echo "land: cannot create the temporary worktree" >&2; exit 1; }; \
			[ -d "$$tmp/wt" ] || { echo "land: the temporary worktree was not created" >&2; exit 1; }; \
			cd "$$tmp/wt" || exit 1; \
		else \
		if [ "$$(printf '%s\n' "$$found" | wc -l | tr -d ' ')" != 1 ]; then echo "land: $$wb is checked out in more than one worktree: stop and tell the human" >&2; exit 1; fi; \
		case "$$found" in "P "*) echo "land: the worktree of $$wb is prunable (its directory is gone): stop and tell the human" >&2; exit 1;; esac; \
		wt="$$(printf '%s' "$${found#W }" | tr '\001' '\n')"; \
		[ -d "$$wt" ] || { echo "land: the worktree of $$wb is missing" >&2; exit 1; }; \
		cd "$$wt" || exit 1; \
		if [ "$$(git symbolic-ref -q --short HEAD)" != "$$wb" ]; then echo "land: the worktree $$wt does not have $$wb checked out" >&2; exit 1; fi; \
		fi; \
	fi; \
	shared="$$(dirname "$$(git rev-parse --path-format=absolute --git-common-dir)")"; \
	if [ -n "$$det" ]; then branch="$$wb"; else branch="$$(git symbolic-ref -q --short HEAD)"; fi || { echo "land: check out the branch to land first" >&2; exit 1; }; \
	if [ -n "$$wb" ] && [ "$$branch" != "$$wb" ]; then echo "land: the worktree switched away from $$wb: stop and tell the human" >&2; exit 1; fi; \
	if [ "$$branch" = main ]; then echo "land: run it on a topic branch in your own worktree, not on main" >&2; exit 1; fi; \
	if [ "$$(git -C "$$shared" symbolic-ref -q HEAD)" != refs/heads/main ]; then \
		echo "land: the shared checkout $$shared is not on main: stop and tell the human (never switch it yourself)" >&2; exit 1; fi; \
	base="$$(git rev-parse --verify refs/heads/main)"; \
	if [ -n "$$short" ] && [ "$$base" != "$$confirm_base" ]; then echo "land: main moved since confirmation: run make land again" >&2; exit 1; fi; \
	candidate="$$(git rev-parse --verify HEAD^{commit})" || { echo "land: cannot read the candidate commit" >&2; exit 1; }; \
	if [ -n "$$want" ] && [ "$$candidate" != "$$want" ]; then echo "land: the candidate is $$candidate, not the requested SHA $$want: refusing" >&2; exit 1; fi; \
	git merge-base --is-ancestor "$$base" "$$candidate" || { echo "land: $$branch is not on top of main: git rebase main first" >&2; exit 1; }; \
	merges="$$(git rev-list --merges "$$base".."$$candidate")" || { echo "land: cannot read the candidate history" >&2; exit 1; }; \
	if [ -n "$$merges" ]; then echo "land: $$branch introduces merge commits: rebase to a linear history before landing" >&2; exit 1; fi; \
	$(LAND_CLEAN) $(LAND_MAKE) -s check-local commitlint || exit 1; \
	$(LAND_CLEAN) $(LAND_MAKE) -s test-commitlint-consumers || exit 1; \
	$(LAND_CLEAN) $(LAND_MAKE) -s secrets-range RANGE="$$base..$$candidate" TIP="$$candidate" || exit 1; \
	generated="$$(git diff --name-only "$$base" "$$candidate" -- 'internal/web/*.templ' 'internal/web/*_templ.go')" || exit 1; \
	generated_check=0; if [ -n "$$generated" ]; then $(LAND_CLEAN) $(LAND_MAKE) -s check-generated || exit 1; generated_check=1; fi; \
	if { [ -z "$$det" ] && [ "$$(git symbolic-ref -q --short HEAD)" != "$$branch" ]; } || [ "$$(git rev-parse --verify HEAD^{commit})" != "$$candidate" ] || [ "$$(git rev-parse --verify "refs/heads/$$branch^{commit}")" != "$$candidate" ]; then \
		echo "land: candidate moved during the checks: run make land again on the intended unchanged branch" >&2; exit 1; fi; \
	if [ "$$(git rev-parse --verify refs/heads/main)" != "$$base" ]; then echo "land: main moved during the checks: git rebase main and run make land again" >&2; exit 1; fi; \
	if [ "$$(git -C "$$shared" symbolic-ref -q HEAD)" != refs/heads/main ]; then echo "land: the shared checkout left main during the checks: stop and tell the human" >&2; exit 1; fi; \
	scripts/index-state.sh "$$shared"; state=$$?; \
	if [ "$$state" = 3 ]; then \
		echo "land: the shared checkout's index is stale (every path that differs from HEAD equals HEAD in the tree): repair it with: git -C $$shared reset -q -- <files shown by git -C $$shared diff --cached --name-only HEAD>" >&2; exit 1; fi; \
	if [ "$$state" != 0 ] && [ "$$state" != 4 ]; then echo "land: cannot read the shared checkout's index" >&2; exit 1; fi; \
	git -C "$$shared" merge -q --ff-only "$$candidate" || exit 1; \
	if [ -n "$$short" ]; then \
		sh -c "$$lsh" land.sh record "$$candidate" "$$branch" "$$confirm_mode" "$$confirm_answer" "$$confirm_at" "$$confirm_review" "$$confirm_base" "$$confirm_class" "$$generated_check" || { echo "land: main moved to $$candidate, but its confirmation note was not recorded: stop and tell the human" >&2; exit 1; }; \
	fi; \
	echo "land: main is now $$(git rev-parse --short refs/heads/main)"; \
	if [ "$$state" = 0 ]; then scripts/index-state.sh "$$shared" || { echo "land: main moved, but the shared checkout's index differs from HEAD after the merge: repair it with: git -C $$shared reset -q -- <files shown by git -C $$shared diff --cached --name-only HEAD>" >&2; exit 1; }; fi

# Scan the commits of a git log range for secrets, their changes and their
# messages, and the message of the annotated tag TIP (gitleaks git reads patches
# only, #196): the pre-push hook runs it with the range about to be pushed. Exit 42 is a finding, 0 is clean, and
# anything else (a failed install, gitleaks' own exit 1) is a scan that could not run.
secrets-range:
	@test -n "$(RANGE)" || { echo "secrets-range needs RANGE" >&2; exit 2; }
	@git rev-list $(RANGE) >/dev/null 2>&1 || { echo "the secret scan could not run (git cannot read the range $(RANGE): fetch the remote first), so nothing was checked and the push is blocked" >&2; exit 1; }
	@d=$$(mktemp -d) || { echo "the secret scan could not run (no temporary directory), so nothing was checked and the push is blocked" >&2; exit 1; }; \
	trap 'rm -rf "$$d"' EXIT; trap 'exit 1' HUP INT TERM; \
	GOBIN="$$d" go install $(GITLEAKS) >&2 || { echo "the secret scan could not run (gitleaks did not install), so nothing was checked and the push is blocked: put go on the PATH of the tool that pushes and let it fetch gitleaks (or push from the terminal), then push again" >&2; exit 1; }; \
	scripts/messages.sh "$(TIP)" $(RANGE) >"$$d/messages" || { echo "the secret scan could not run (git cannot read the commit and tag messages of $(RANGE)), so nothing was checked and the push is blocked" >&2; exit 1; }; \
	"$$d/gitleaks" git --no-banner --redact --exit-code $(GITLEAKS_FOUND) --config .gitleaks.toml --log-opts="$(RANGE)" . >&2; rc=$$?; \
	"$$d/gitleaks" stdin --no-banner --redact --exit-code $(GITLEAKS_FOUND) --config .gitleaks.toml <"$$d/messages" >&2; rcm=$$?; \
	if [ $$rc -eq 0 ] && [ $$rcm -eq 0 ]; then exit 0; \
	elif [ $$rc -eq $(GITLEAKS_FOUND) ] || [ $$rcm -eq $(GITLEAKS_FOUND) ]; then echo "a commit or tag message, or a change, in $(RANGE) holds a secret: revoke it, remove it from the history, then push (AGENTS.md, Secrets)" >&2; \
	else echo "the secret scan could not run (exit $$rc and $$rcm), so nothing was checked and the push is blocked: check the output above, then push again" >&2; fi; \
	exit 1

# Scan what is staged for secrets: the pre-commit hook runs it.
secrets-staged:
	@d=$$(mktemp -d) || { echo "the secret scan could not run (no temporary directory), so nothing was checked and the commit is blocked" >&2; exit 1; }; \
	trap 'rm -rf "$$d"' EXIT; trap 'exit 1' HUP INT TERM; \
	GOBIN="$$d" go install $(GITLEAKS) >&2 || { echo "the secret scan could not run (gitleaks did not install), so nothing was checked and the commit is blocked: put go on the PATH of the tool that commits and let it fetch gitleaks (or commit from the terminal), then commit again" >&2; exit 1; }; \
	"$$d/gitleaks" git --pre-commit --staged --no-banner --redact --exit-code $(GITLEAKS_FOUND) --config .gitleaks.toml . >"$$d/out" 2>&1; rc=$$?; \
	if [ $$rc -eq 0 ]; then exit 0; \
	elif [ $$rc -eq $(GITLEAKS_FOUND) ]; then echo "a secret is staged: unstage it, revoke it if it is real, and read it from a 0600 env file instead (AGENTS.md, Secrets)" >&2; cat "$$d/out" >&2; \
	else echo "the secret scan could not run (exit $$rc), so nothing was checked and the commit is blocked: check the output below, then commit again" >&2; cat "$$d/out" >&2; fi; \
	exit 1
