# Workspace configuration repository example

This is a layout example, not a real project submodule or a provisionable fixture.
Create a separate Git repository from this directory, then add your actual project:

```sh
git init -b main
git submodule add -- https://YOUR-FORGE/YOUR-PROJECT.git projects/app
git add .
git commit -m 'chore: define development workspace'
```

Git records the project commit. Authenticate through credential helpers, not URLs.
The devcontainer example selects a language image; before real use replace its tag
with a verified digest. No fake digest is supplied. Bundles are optional and empty.
The platform manifest is WorkHarbor's proposed schema, not a Dev Container field.

## Current provider subset

The historical top-level Dev Container example uses a tag and named user; it is not
admitted by the initial T009 provider subset. Separately identified
[image](admitted-image/README.md) and [Dockerfile](admitted-dockerfile/README.md)
inputs use a pinned digest and explicit numeric users. Their real provider
verification is tracked in the [capability matrix](../../docs/development/devcontainer-matrix.md).
