# Tool versions are pinned; tools run via `go run`, so nothing needs installing.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
EDITORCONFIG_CHECKER := github.com/editorconfig-checker/editorconfig-checker/v3/cmd/editorconfig-checker@v3.11.3

.PHONY: build test vet fmt fmt-check lint editorconfig check commitlint changelog docs docs-serve hooks

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

# Check commits on this branch that are not on origin/main.
commitlint:
	go run ./cmd/commitlint --range origin/main..HEAD

# Regenerate CHANGELOG.md from Conventional Commits (git-cliff via npx).
changelog:
	npx --yes git-cliff@2 --output CHANGELOG.md

# Build the documentation site into _site (needs Ruby and Bundler).
docs:
	cd docs && bundle install && bundle exec jekyll build --destination ../_site

# Serve the documentation site locally with live reload.
docs-serve:
	cd docs && bundle install && bundle exec jekyll serve --livereload

# Enable the repository git hooks and the commit message template.
hooks:
	git config core.hooksPath .githooks
	git config commit.template .gitmessage
