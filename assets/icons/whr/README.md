# whr container icons

SVG icons for whr containers in container UIs such as Arcane. The files are original work of the repository owner and
fall under the repository license. They contain no third-party logos; the badges are plain letters.

## Files

`whr.svg` is the base logo. `whr-<role>-<distro>.svg` covers the matrix below. `whrx-arcane.svg` is the extension icon.

| Role  | Glyph tint        | debian | ubuntu | alpine |
|-------|-------------------|--------|--------|--------|
| dev   | teal `#14b8a6`    | yes    | yes    | yes    |
| test  | amber `#f59e0b`   | yes    | yes    | yes    |
| build | slate `#7c8da5`   | yes    | yes    | yes    |

Badges (bottom-right corner, white letter drawn as a path): debian red `#d70a53` with `D`, ubuntu orange `#e95420`
with `U`, alpine blue `#0d597f` with `A`, arcane violet `#7c3aed` with `X`. `whr.svg` and `whrx-arcane.svg` use the base
tint blue `#3b82f6`.

Each file is 64x64, uses mid-tone colors with a dark outline so one file reads on light and dark backgrounds, and has no
scripts, fonts, images or external references. Each file is at most 2 KB.

## Glyph status

The anchor is a placeholder. The glyph comes from one place, `GLYPH_PATHS` and `GLYPH_RING` in `generate.py`; replacing
it and regenerating changes every icon.

## Generating

```sh
python3 assets/icons/whr/generate.py
```

The script writes all files from its tables and is deterministic. It does not delete files; removing a role or distro
means deleting its file by hand. The generated files are committed.
`tests/test_whr_icons.py` checks the matrix, the XML structure, the forbidden content, the size limit, the badges and
that regeneration reproduces the committed bytes.

## Reference from whr

```text
https://cdn.jsdelivr.net/gh/wstein/workharbor@<commit-sha>/assets/icons/whr/<file>.svg
```

`<commit-sha>` is the full 40-character commit SHA. A branch URL is served with `cache-control: public, max-age=604800,
s-maxage=43200` (12 hours at the CDN, up to 7 days in a browser); a commit URL with `max-age=31536000, immutable`. The URL
resolves for any pushed commit SHA. Main is merged by rebase, so use the SHA of the commit on main that contains the
files, not the branch SHA. Container labels are fixed at creation, so a changed icon needs a new SHA in the label, which recreates the
containers that carry it.
