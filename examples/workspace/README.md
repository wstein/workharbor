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
