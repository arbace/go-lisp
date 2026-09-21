# go-lisp

This is a fork of golang/go that adds a second front end to the gc compiler.
It accepts the whole Go language written as Clojure/EDN-style s-expressions.
The design lives in `golisp/DESIGN.md`, and the normative per-node grammar in
`golisp/SPEC.md`. It is still being worked out with the
user, one iteration at a time. Read it first, and ask the user before settling
any open decision (D1, D2, ...). When a decision is settled, record it in the
"Decided" section of that file.

## Git rules (hard)

- Commit and push **only** on the `go-lisp` branch. It is the default branch
  of `origin` (github.com/arbace/go-lisp, private).
- **Never** commit to, push to, or merge into `master`. `master` mirrors
  upstream golang/go.
- Commit messages follow Go style: `pkg/path: lowercase summary`, with the
  prefix `golisp:` for docs and tooling in this repo.

## Keep upstream files untouched

The goal is painless merges from upstream golang/go.
- Put new code in new files and directories:
  new `lisp_*.go` files in `src/cmd/compile/internal/syntax/` for the reader,
  parser and printer (in-package, so they can reuse unexported helpers),
  and `golisp/` for docs and specs.
- If an existing upstream file must change, keep the edit as small as
  possible (a one-line hook) and mark it with a `// go-lisp:` comment, so
  `git grep 'go-lisp:'` lists every intrusion.
- Don't reformat or refactor upstream code.

## Architecture in brief

- The Lisp parser produces the same `*syntax.File`
  (`src/cmd/compile/internal/syntax/nodes.go`) as the Go parser.
  types2 and everything after it stay unchanged.
- Compiler entry point: `syntax.Parse` call in
  `src/cmd/compile/internal/noder/noder.go` (the file loop in `LoadPackage`).
- The Go -> Lisp -> Go mapping must be lossless. Correctness is tested by
  round-tripping `$GOROOT/src` and `$GOROOT/test` ASTs through the Lisp printer
  and parser.

## Building and testing

- The bootstrap toolchain is the system `go` (1.27). Build this tree with
  `cd src && ./make.bash`. Afterwards, use `bin/go` from the repo root, not
  the system `go`, to test compiler packages.
- Fast loop: `bin/go test -short cmd/compile/internal/syntax -run Lisp`.
  Without `-short`, the corpus tests print and round-trip all of `$GOROOT`
  (about 1 minute).
- Behavioral test (slow, about 5 minutes):
  `bin/go test cmd/compile/internal/syntax -run TestLispRunCorpus -lisprun`.
- Print any Go file as go-lisp:
  `bin/go test cmd/compile/internal/syntax -run TestLispPrintFile -lispsrc FILE.go`.
- Compile a go-lisp file: `bin/go tool compile -p main -importcfg CFG -o x.o x.lgo`.
  The only upstream hook is in `noder/noder.go` (`git grep 'go-lisp:'`).
- After changing the compiler, rebuild the toolchain with
  `bin/go install cmd/compile`, or rerun `make.bash`.
