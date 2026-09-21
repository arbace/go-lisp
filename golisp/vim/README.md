# go-lisp for Vim and Neovim

Editing support for go-lisp (`.lgo`) files, in plain Vim script, so it
works in both Vim and Neovim.

## What you get

- **Filetype detection** for `*.lgo`.
- **Syntax highlighting:**
  - Go keywords as special forms, keyword forms (`:=`, `:index`, `:lit`, …),
    the `:_` marker, and field keys
  - operators at the head of a list
  - predeclared types, builtins, and constants
  - declared names after `func` and `type`
  - strings with Go escapes, raw strings, runes, and Go number formats
  - `;` comments, with `;go:` and `;line` directives highlighted separately
- **Indentation** matching the layout of the go-lisp printer
  (`go tool golisp go2lisp`):
  - the bodies of `func`, `if`, `for`, `case`, `struct`, `:lit`, and other
    special forms are indented by `shiftwidth` (2) from the line where the
    form starts
  - call arguments align with the first argument
  - lines inside multi-line raw strings are never changed

  `gg=G` reproduces the printer's layout exactly (tested against real
  files in `cmd/compile/golisp/vim_test.go`).
- **Buffer settings:** `;` comments (`gc`-style plugins use
  `commentstring`), lisp-style word motions (`read-all`, `-count`, `x.-f`
  and `:=` are single words), and 2-space indentation.
- **`:make`** runs `go build .` and puts compile errors in the quickfix
  list, with positions in the `.lgo` source.
- **Commands:**
  - `:GolispRun [args]` runs the main package in the current directory
    (`go run .`)
  - `:GolispToGo` shows the current file as Go in a new window
  - `:GolispFromGo file.go` shows a Go file as go-lisp in a new window

  The two conversion commands need the golisp tool:
  `go install cmd/compile/golisp`.

The `go` command on your `PATH` must be the go-lisp toolchain
(`bin/go` of this repository).

## Installation

Vim (native packages):

```sh
mkdir -p ~/.vim/pack/golisp/start
ln -s /path/to/go-lisp/golisp/vim ~/.vim/pack/golisp/start/golisp
```

Neovim:

```sh
mkdir -p ~/.local/share/nvim/site/pack/golisp/start
ln -s /path/to/go-lisp/golisp/vim ~/.local/share/nvim/site/pack/golisp/start/golisp
```

Or add the directory to `runtimepath` in your vimrc or init.vim:

```vim
set runtimepath^=/path/to/go-lisp/golisp/vim
```

Make sure `filetype plugin indent on` and `syntax on` are set (Neovim
does this by default).
