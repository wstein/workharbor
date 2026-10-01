# Spike #80: npm over a volume mounted at node_modules

`run.sh` runs on Apple Container 1.5.0 with `node:22-slim`: a volume at `/work/node_modules` over the bind-mounted `/work`. `results.txt` is its unedited output (the script keeps only the last lines of each step).

| Case | Result |
| --- | --- |
| `npm install` into the fresh volume | exit 0; `lost+found` is not in the listing afterwards |
| `npm ci` over an existing tree | exit 0 |
| `rm -rf node_modules && npm install` | `rm` exits 1 (`Device or resource busy`, the contents are removed); `npm install` then exits 0 |
| `npm ci` after the directory was emptied by hand | exit 0 |
| host view of `node_modules` | 0 entries |

npm clears the contents of an existing `node_modules` and does not remove the directory itself, so the busy mount point does not break `npm ci` or `npm install`. Only a literal `rm -rf node_modules` reports an error, and the next install still works. Limits: the npm in `node:22-slim` (its version was not recorded), two small packages, one run; `pnpm` and `yarn` were not tried.
