# Vendored browser libraries

The page loads no library from the network: everything third-party lives in this directory, is embedded in the `sil` binary with the rest of `ui/web` and is served from `/vendor/`. There is no build step. Only uPlot is vendored (the page is vanilla ES modules; Alpine is not used).

## uPlot

| field | value |
|-------|-------|
| library | uPlot |
| version | 1.6.32 |
| source | `npm pack uplot@1.6.32 --ignore-scripts` (files `package/dist/uPlot.iife.min.js`, `package/dist/uPlot.min.css`, `package/LICENSE`), vendored once, never installed at build or run time |
| upstream | https://github.com/leeoniya/uPlot |
| license | MIT (compatible with this project's GPL-3.0); text in `LICENSE.uplot` |
| npm integrity (tarball) | `sha512-KIMVnG68zvu5XXUbC4LQEPnhwOxBuLyW1AHtpm6IKTXImkbLgkMy+jabjLgSLMasNuGGzQm/ep3tOkyTxpiQIw==` |

The tarball sha512 was checked against `npm view uplot@1.6.32 dist.integrity` before extraction; the sha256 of each file below was checked against the values recorded in the phase research.

## Files

`TestVendorHashes` (run by `make check`) re-hashes every file in this table from the embedded file system and fails on a size or hash difference, on a file in `vendor/` that is not listed here, and on an empty table.

| file | bytes | sha256 |
|------|-------|--------|
| uPlot.iife.min.js | 51081 | 19c8d4c6ad88929a79f4ae49d6f7161566dfd0ba3d15cc495e974f787eb78f1f |
| uPlot.min.css | 1857 | df630c6a8d6f8eeaff264b50f73ce5b114f646ffd9a0bb74f049b0a00135fa04 |
| LICENSE.uplot | 1078 | 8f989229699b4fe2f1a0432d0e9edc338a8a911e250e2d1b01ecd770a5f5b1bd |

## Scan exclusion

`TestNoExternalRefs` scans first-party files for `http://` and `https://` and skips this directory by design: `uPlot.iife.min.js` carries a credit comment with the upstream URL, which is never fetched. The vendored files are covered by the hashes above instead.

## Bumping

Replace the three files from a new `npm pack`, update the version, integrity and table rows here, and run `make check`.
