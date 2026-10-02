# workharbor brand rule

One rule for every asset, so the banner, the social preview, the docs navbar and
the icons look like one product (#142). Name and casing: AGENTS.md, Conventions.

## Two forms, never mixed

- **App icon** (square places only: `favicon.svg`, `favicon.ico`, the
  `favicon-*.png`, `apple-touch-icon.png`, the `android-chrome-*.png`,
  `logo.svg`/`logo.png`): the teal anchor on a rounded navy tile. Tile corner
  radius 22 % of the side; the anchor's drawn height 70 % of the side, centred, so it stays legible at 16 px. `apple-touch-icon.png` is the exception: an opaque, full-bleed navy square with no rounded corners and no transparency, because iOS applies its own mask and fills transparent pixels with black.
- **Horizontal lockup** (everything wide: the README banner, the social preview,
  the docs navbar): the bare teal anchor, no tile, then the wordmark. A tile never
  appears in a lockup, and a lockup never appears in a square place.

## Exception: the README banner and the social preview

By Werner's decision (#143), these two assets keep the design of d466a1d:
the anchor on its rounded tile beside the wordmark, "Harbor" in teal, and the
IBM Plex Sans tagline. The tile is allowed there as an exception to "a tile never
appears in a lockup": 96 px in the banner (64 units at scale 1.5) and 288 px in
the social preview (scale 4.5). Still no third line: the social preview drops
"Self-hosted · isolated workspaces · whr CLI". The docs navbar keeps the bare
anchor, and every icon and logo follows the rules below; it stays one family
(the same anchor, tile, wordmark, colours and type).

## The lockup, in proportions of the wordmark's cap height C

| Element | Rule |
| --- | --- |
| Wordmark | "WorkHarbor" in Bricolage Grotesque Bold, outlined: "Work" in the text colour, "Harbor" in teal |
| Anchor | drawn height 2.4 × C, vertically centred on the wordmark's cap height; stroke 5.5 in the anchor's 64-unit drawing (4 in the app icon), so it holds its own beside the bold wordmark |
| Gap anchor to wordmark | 0.6 × C |
| Tagline (optional) | "Supervise AI coding agents. Stay in the loop.", IBM Plex Sans, outlined, cap height 0.4 × C, teal; baseline 0.85 × C below the wordmark's baseline, left-aligned with the wordmark |
| Third line | none, anywhere |
| Clear space | 0.6 × C to the left and right; at least 0.45 × C above and below the whole block (anchor and text), except in the README banner, where its fixed 150 px height allows 0.33 × C |

## Sizes

| Asset | Size | C | Tagline |
| --- | --- | --- | --- |
| README banner | 1000 × 150 | 45 px (anchor 108 px as Werner approved; the block leaves about 15 px, 0.33 × C, above and below) | yes |
| Social preview | 1280 × 640, lockup centred | 80 px (anchor 192 px) | yes |
| Docs navbar | the navbar's height | 0.4 × navbar height (anchor about 54 px) | no |

## Colours

Navy `#0b2545` background with white text and teal `#5eead4`; on a light
background, navy `#0b2545` text and deep teal `#0f766e`. No gradient other than
the banner's existing one, no other colours.

Every asset is regenerated from its SVG; a PNG is never edited by hand. A change
to this rule, or to an asset, is shown to Werner as one comparison image of all
assets before it lands.
