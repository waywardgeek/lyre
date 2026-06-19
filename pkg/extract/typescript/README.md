# TypeScript extractor

This package wraps the TypeScript Compiler API (via `extract_api.js`) to
produce `extract.PackageInfo` from `.ts` source for the v2 `.lyric` pipeline.

## Dependencies

- `node` (Node.js 18+)
- `npm` (for first-time setup)

## Setup

`node_modules/` is gitignored. On first use, `runExtractScript` will run
`npm install` automatically in this directory. The dependency list
(`package.json` / `package-lock.json`) is committed.

If you want to run `npm install` yourself ahead of time:

```sh
cd pkg/extract/typescript
npm install
```

## Public API

- `ExtractTs(srcDir) → *extract.PackageInfo`
- `GenerateTs(srcDir) → (outPath, content, err)`
- `UpdateTs(lyricPath) → (added, err)`
- `VerifyTs(lyricPath) → (*VerifyResult, err)`

These mirror the Phase 3a Go-extractor API exactly. See
`cr/docs/rich-doc-upgrade-plan.md` for the sprint context.
