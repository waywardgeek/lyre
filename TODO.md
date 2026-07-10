# Lyre TODOs

(Cleared 2026-06-22: the embedded-field parser bug, the test-only Go dir
exclusion, and TypeScript .tsx support — all three were fixed in this
session and have regression tests / empirical CR-repo validation.)

## Known polish items

- TypeScript `async` detection in `extract_api.js`. The `.lyric` DSL now
  round-trips an `async` decl-line prefix (`async func`/`async method`) via
  `FuncInfo.IsAsync`, and the Python extractor sets it (2026-07-10). Go has no
  async modifier, but TypeScript does — `extract_api.js` should emit
  `is_async` for `async` functions/methods so `.ts.lyric` faithfully documents
  coroutines. Until then, TS async functions are documented as plain
  `func`/`method` (subtly incomplete, same gap Python had). Do this when the
  first TypeScript workflow/orchestration code appears.

- `lyre gen` on directories with React FC components scaffolds them as
  `typedef X: React.FC<Props>` rather than as functions with extracted
  props. Not a bug — `React.FC` IS the canonical type — but the .lyric is
  less informative than for pure-Go function exports. Improving this
  requires teaching extract_api.js to special-case `React.FC` / arrow-
  function-typed const exports and extract the props shape.

- `lyre update` for legacy plain-.lyric files (runUpdate in
  cmd/lyre/main.go) is a stub. Plain .lyric files in old Forge syntax
  are vanishingly rare and will be migrated in Phase 6 of the rich-doc
  upgrade.
