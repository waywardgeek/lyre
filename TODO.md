# Lyre TODOs

(Cleared 2026-06-22: the embedded-field parser bug, the test-only Go dir
exclusion, and TypeScript .tsx support — all three were fixed in this
session and have regression tests / empirical CR-repo validation.)

## Known polish items

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
