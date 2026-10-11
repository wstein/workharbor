# Anchorage bootstrap completion

Date: 2026-10-11. Scope: Git project bootstrap and portable full-history bundle.

## Delivered
Pinned upstream Spec Kit/Codex infrastructure, constitution v1, product and
architecture, five ADRs, domain/provider/bundle contracts, a Git workspace example,
feature 001 specification/plan/research/model/quickstart/tasks, repository harness,
CI, Symphony workflow and secret-safe operator preparation. Focused Conventional
Commits retain the design and setup sequence. No remote was created or pushed.

## Validation evidence
A candidate full-history Git bundle was cloned into a fresh directory. In that
clone, `make check` passed repository navigation/JSON/boundary checks, all six
regression tests, shell syntax and upstream Spec Kit prerequisites. Checkout-local
feature selection followed by upstream prerequisite inspection also passed.
`git fsck --full` passed; tracked working tree stayed clean. `git bundle verify`
reported complete history with no prerequisite repository. The final export
includes this record and is verified again before delivery.

## Remaining implementation and activation
Feature 001 tasks T001–T010 are all pending. No Phoenix or Go application, Docker
runtime, browser session or resource benchmark has been implemented/tested.
Symphony workflow front matter and preparation behavior are validated, but its
Elixir runtime and Codex app-server/tracker interaction were not run. The operator
must supply a trusted repo, isolated workspace root, Linear project and secret,
then install/authenticate the pinned development tools using the runbook. Project
license remains an explicit owner decision. Bundledock's real artifact format must
be verified before feature 002. These limits are intentional bootstrap scope.

## Re-export
From a clean checkout:

```sh
git bundle create /absolute/output/anchorage.bundle --all
git bundle verify /absolute/output/anchorage.bundle
git clone /absolute/output/anchorage.bundle /absolute/fresh-checkout
```

Run `make check` there. A bundle contains committed Git objects, not ignored local
feature pointers, credentials, installed runtimes or workspaces.
