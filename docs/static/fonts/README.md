# Self-hosted fonts

The docs site loads these files from its own origin; no page requests a font
host. All three families are licensed under the SIL Open Font License 1.1, and
each licence text sits next to its files.

| Family | Use | Files | Source | Licence |
| --- | --- | --- | --- | --- |
| IBM Plex Sans | docs body text | `ibm-plex-sans/` (woff2: Regular, Italic, Medium, SemiBold, Bold) | [github.com/IBM/plex](https://github.com/IBM/plex), release `@ibm/plex-sans@1.1.0`, `ibm-plex-sans.zip`, `fonts/complete/woff2/` | OFL 1.1, `ibm-plex-sans/OFL.txt` (`LICENSE.txt` of the release) |
| JetBrains Mono | code and commands | `jetbrains-mono/` (woff2: Regular, Bold) | [github.com/JetBrains/JetBrainsMono](https://github.com/JetBrains/JetBrainsMono), release `v2.304`, `JetBrainsMono-2.304.zip`, `fonts/webfonts/` | OFL 1.1, `jetbrains-mono/OFL.txt` |
| Bricolage Grotesque | the wordmark, as outlines only | `../../../assets/fonts/bricolage-grotesque/` (variable TTF, renamed from the upstream `BricolageGrotesque[...].ttf`) | [github.com/google/fonts](https://github.com/google/fonts) at commit `6ce172f74aa355ea43eb964fa4a91570a4d3064d`, `ofl/bricolagegrotesque/`; upstream [github.com/ateliertriay/bricolage](https://github.com/ateliertriay/bricolage) has no release | OFL 1.1, `OFL.txt` next to the TTF |

Bricolage Grotesque is not served by the site: the wordmark is converted to
outlines in the SVG and PNG files, so it sits in `assets/fonts/`, not here.

## Checksums

The upstream releases publish no checksums, so these are SHA-256 values of the
files as downloaded, to detect a later change (not a verification against the
publisher).

```text
fb365d910566e6d199cc2c15579a7dd9a267128e18431a394ed81f1970c69200  ibm-plex-sans.zip (release archive)
6f6376c6ed2960ea8a963cd7387ec9d76e3f629125bc33d1fdcd7eb7012f7bbf  JetBrainsMono-2.304.zip (release archive)
413e7357809ddd12fd80a96a8a396de0e401638d4acd3cb3e37532f0472ac682  BricolageGrotesque-Variable.ttf
fa7130d854a660b39a7fc9e6e0f2dc23dba5f1346e2adea3e1fe37b6d884133d  ibm-plex-sans/IBMPlexSans-Bold.woff2
13284fab1821ba6e3652c1580fcf2bbfd8c9309520c69b3d1224dab40b37c597  ibm-plex-sans/IBMPlexSans-Italic.woff2
5660f8a658f8bb50dbc005232f885eadffd2bc1c235c4f6fbb63469d1f9cde6d  ibm-plex-sans/IBMPlexSans-Medium.woff2
ba711a3085ff9f27440b6b9c4550cfc47c97bf36591d5da958b975bb3add8c1a  ibm-plex-sans/IBMPlexSans-Regular.woff2
f78048030eab62e860efa39a0df79e2e5581bf122eb95b9bc42c0b8a4988d205  ibm-plex-sans/IBMPlexSans-SemiBold.woff2
c503cc5ec5f8b2c7666b7ecda1adf44bd45f2e6579b2eba0fc292150416588a2  jetbrains-mono/JetBrainsMono-Bold.woff2
a9cb1cd82332b23a47e3a1239d25d13c86d16c4220695e34b243effa999f45f2  jetbrains-mono/JetBrainsMono-Regular.woff2
```

The licence texts are the upstream files with only trailing whitespace and line endings normalised, to satisfy `.editorconfig`.
