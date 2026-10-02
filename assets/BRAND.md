# workharbor brand rule

One rule for every asset, so the banner, the social preview, the docs navbar and
the icons look like one product (#142). Name and casing: AGENTS.md, Conventions.

## Two forms, never mixed

- **App icon** (square places only: `favicon.svg`, `favicon.ico`, the
  `favicon-*.png`, `apple-touch-icon.png`, the `android-chrome-*.png`,
  `logo.svg`/`logo.png`): the teal anchor on a rounded navy tile. Tile corner
  radius 22 % of the side; the anchor's drawn height 60 % of the side, centred.
- **Horizontal lockup** (everything wide: the README banner, the social preview,
  the docs navbar): the bare teal anchor, no tile, then the wordmark. A tile never
  appears in a lockup, and a lockup never appears in a square place.

## The lockup, in proportions of the wordmark's cap height C

| Element | Rule |
| --- | --- |
| Wordmark | "WorkHarbor" in Bricolage Grotesque Bold, outlined: "Work" in the text colour, "Harbor" in teal |
| Anchor | drawn height 2.0 × C, vertically centred on the wordmark's cap height |
| Gap anchor to wordmark | 0.6 × C |
| Tagline (optional) | "Supervise AI coding agents. Stay in the loop.", IBM Plex Sans, outlined, cap height 0.4 × C, teal; baseline 0.85 × C below the wordmark's baseline, left-aligned with the wordmark |
| Third line | none, anywhere |
| Clear space | 0.6 × C on every side, and the left margin |

## Sizes

| Asset | Size | C | Tagline |
| --- | --- | --- | --- |
| README banner | 1000 × 150 | 45 px (anchor 90 px) | yes |
| Social preview | 1280 × 640, lockup centred | 80 px | yes |
| Docs navbar | the navbar's height | 0.4 × navbar height | no |

## Colours

Navy `#0b2545` background with white text and teal `#5eead4`; on a light
background, navy `#0b2545` text and deep teal `#0f766e`. No gradient other than
the banner's existing one, no other colours.

Every asset is regenerated from its SVG; a PNG is never edited by hand. A change
to this rule, or to an asset, is shown to Werner as one comparison image of all
assets before it lands.
