---
title: JSON Schemas
description: Provisional JSON Schemas for config.json and the devcontainer customizations.workharbor block, for editor completion.
weight: 19
toc: true
---

{{< status unverified >}} **Provisional until the beta: the configuration is not
stable, and so are these schemas.** They are an editor aid for catching a typo
or a wrong type before `whr doctor` does. They never decide anything: `whr
doctor` and `whr serve` decide whether a configuration is valid, including
file ownership, permissions and every runtime rule. A schema checks structure
only: keys, types, enums and plain ranges.

## The published schemas

The docs site publishes the hand-written schemas from the repository's
`schemas/` directory, copied unchanged during the docs build (a test fails when
the published file differs from the source), next to
[`openapi.json`](api-reference.md).

| Surface | URL |
| --- | --- |
| `config.json` | `https://wstein.github.io/workharbor/schemas/config.v0-provisional.schema.json` |
| `customizations.workharbor` in `devcontainer.json` | `https://wstein.github.io/workharbor/schemas/devcontainer-workharbor.v0-provisional.schema.json` |

**Versions.** `v0-provisional` is the name of the configuration these files
map: the known structure as of 2026-10. A published file never changes once a
stable name is chosen; the provisional files may still be corrected until then
(no one relies on them yet). When the configuration changes, the schema gets a
new name (`v1-provisional`, later a stable `v1`) and the old file and its URL
stay, so a file that names an older version keeps working in an editor. That a
published file is never edited is a rule of review; no test checks it against
history.

**How they are kept honest.** A Go test compares each schema recursively with
the Go struct the product reads (every key at every depth on both sides, types
and optionality), and fails with the key's path. Further tests validate the
manual's example and deliberately wrong ones (a nested typo, a wrong type, an
unknown enum value, a port out of range). Schemas are written by hand; there is
no generator.

## `$schema` in config.json

Add the URL as the first key to get completion and warnings in an editor that
reads `$schema` (many do for JSON files):

```json
{
  "$schema": "https://wstein.github.io/workharbor/schemas/config.v0-provisional.schema.json",
  "listen": "127.0.0.1:8787"
}
```

The loader accepts `$schema` as an optional string and ignores it. Nothing in
WorkHarbor downloads or checks the URL, so the supervisor works offline and a
missing, wrong or unreachable value never changes loading. Every other unknown
key is still refused by name. (In the schema document itself, `$schema` means
something else: the JSON Schema dialect, a different URL.) `whr setup` does not
write the line yet; that is a separate issue (#269).

## The `customizations.workharbor` block

An editor only validates a whole file against a schema, and the devcontainer
specification keeps `customizations` free-form, so the block schema is attached
to the path `customizations.workharbor` through a wrapper. With VS Code, put
this in the repository's `.vscode/settings.json`:

```json
{
  "json.schemas": [
    {
      "fileMatch": ["devcontainer.json", ".devcontainer.json"],
      "schema": {
        "properties": {
          "customizations": {
            "properties": {
              "workharbor": {
                "$ref": "https://wstein.github.io/workharbor/schemas/devcontainer-workharbor.v0-provisional.schema.json"
              }
            }
          }
        }
      }
    }
  ]
}
```

Whether VS Code fetches the remote `$ref`, and whether this combines with the
devcontainer schema the editor already applies, were not measured here
({{< status unverified >}}). Other editors were not tried. Only the schema file
itself is tested. The keys describe hints: the supervisor's own configuration
decides, and a repository can only ask (D38), so a value that passes the schema
can still be ignored with a note.
