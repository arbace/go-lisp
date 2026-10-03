# go-lisp

**Go that also compiles Go written as s-expressions.**

go-lisp is a fork of the [Go toolchain](https://github.com/golang/go)
with a second front end for the gc compiler. Next to `.go` files it
compiles `.lgo` files: the whole Go language, written in a
Clojure/EDN-flavored Lisp syntax.

```clojure
(package main)

(import "bufio" "fmt" "os" "sort" "strings")

(func main [] []
  (:= counts (:lit (map string int)))
  (:= sc (bufio.new-scanner os.stdin))
  (for [(sc.scan)]
    (for [_ w (range (strings.fields (sc.text)))]
      (++ (:index counts w))))
  (:= words (make (:slice-of string) 0 (len counts)))
  (for [w (range counts)]
    (= words (append words w)))
  (sort.strings words)
  (for [_ w (range words)]
    (fmt.println w (:index counts w))))
```

An `.lgo` file is parsed into exactly the syntax tree that the Go parser
builds for the equivalent Go. So everything after parsing (type checking,
SSA, the linker) is unchanged, and `.go` and `.lgo` files mix freely, even
within one package. It isn't a new language, and there are no macros: just
another way to write Go. Names are kebab-case and exported by default
(`new-scanner` is `NewScanner`; a leading `-`, as in `-helper`, makes a
name unexported).

## What works

- `go build`, `go run`, `go test` (including `_test.lgo` files) and
  `go test -cover` on packages with `.lgo` files, alone or mixed with
  `.go` files; `go run main.lgo`
- `go tool golisp go2lisp` and `lisp2go` convert in both directions
- Every valid Go file in `$GOROOT/src` and `$GOROOT/test` (11,588 files)
  converts to go-lisp and back to the identical syntax tree; 966 of Go's
  test programs behave the same when compiled from `.lgo`
- Vim/Neovim support

Start with the **[user guide and cheat sheet](../golisp/README.md)**. The
design is in [DESIGN.md](../golisp/DESIGN.md) and the full grammar in
[SPEC.md](../golisp/SPEC.md).

## Relation to Go

- The default branch, `go-lisp`, is upstream Go plus this front end.
  `master` mirrors [golang/go](https://github.com/golang/go) unchanged.
- The new code lives in new files (`lisp_*.go` in
  `cmd/compile/internal/syntax`, `cmd/compile/golisp`, `internal/golisp`,
  `golisp/`). Upstream files have only a handful of one-line hooks, each
  marked `// go-lisp:` (`git grep 'go-lisp:'`), so upstream merges stay easy.
- This is an independent experiment, not part of the Go project. Please
  report bugs in Go itself [upstream](https://go.dev/issue), not here.
  Two such bugs, found while building go-lisp, are in
  [golisp/upstream](../golisp/upstream/README.md). Go's own README is
  [README.md](../README.md).

Sister project: [ghc-lisp](https://github.com/arbace/ghc-lisp), the same
idea for the Glasgow Haskell Compiler.

## Building

Build like Go, with a Go toolchain to bootstrap from:

```sh
cd src && ./make.bash
../bin/go run hello.lgo
```

See [CLAUDE.md](../CLAUDE.md) for the test commands.
