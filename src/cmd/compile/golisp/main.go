// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Golisp converts between Go and go-lisp, and builds and runs go-lisp programs.
//
// Usage:
//
//	go tool golisp go2lisp [-w] file.go...
//	go tool golisp lisp2go [-w] file.lgo...
//	go tool golisp build [-o output] [-gonames] file.lgo|file.go...
//	go tool golisp run [-gonames] file.lgo|file.go... [-- arguments]
//
// go2lisp prints each Go file as go-lisp, and lisp2go prints each go-lisp
// file as gofmt-formatted Go. With -w, the result is written to a file
// next to the input with the other extension instead. go2lisp keeps
// comments (as ';' comments) and //go: directives; it drops line
// directives (//line), and warns about them. lisp2go keeps ;go:
// directives but not other comments.
//
// build compiles and links the given files, which must form a main
// package, into an executable (named after the first file by default).
// run builds a temporary executable and runs it with the arguments
// after "--". Compiler errors in go-lisp files report names as they are
// spelled in the go-lisp source (-count, not count); -gonames reports
// Go names instead. Expressions and types in error messages are always
// shown in Go syntax. Imports are resolved with "go list", so they may name
// standard library packages and packages of the current module.
// Until the go command supports go-lisp, only the files given are
// compiled as go-lisp.
//
// The go-lisp syntax is described in golisp/DESIGN.md and golisp/SPEC.md.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"cmd/compile/internal/syntax"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage: go tool golisp <command> [arguments]

commands:
	go2lisp [-w] file.go...         print Go files as go-lisp
	lisp2go [-w] file.lgo...        print go-lisp files as Go
	build [-o output] [-gonames] files...   build a go-lisp main package
	run [-gonames] files... [-- arguments]  build and run a go-lisp main package
`)
	os.Exit(2)
}

func main() {
	log := func(err error) {
		fmt.Fprintln(os.Stderr, "golisp:", err)
		os.Exit(1)
	}
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	fs.Usage = usage
	switch cmd {
	case "go2lisp", "lisp2go":
		write := fs.Bool("w", false, "write the result next to each input file")
		fs.Parse(args)
		if fs.NArg() == 0 {
			usage()
		}
		for _, file := range fs.Args() {
			if err := convert(cmd, file, *write, os.Stdout, os.Stderr); err != nil {
				log(err)
			}
		}

	case "build":
		out := fs.String("o", "", "output file")
		goNames := fs.Bool("gonames", false, "report errors with Go names instead of go-lisp spellings")
		fs.Parse(args)
		if fs.NArg() == 0 {
			usage()
		}
		exe := *out
		if exe == "" {
			base := filepath.Base(fs.Arg(0))
			exe = strings.TrimSuffix(base, filepath.Ext(base))
			if runtime.GOOS == "windows" {
				exe += ".exe"
			}
		}
		if err := build(goTool(), fs.Args(), exe, !*goNames); err != nil {
			log(err)
		}

	case "run":
		goNames := fs.Bool("gonames", false, "report errors with Go names instead of go-lisp spellings")
		fs.Parse(args)
		args = fs.Args()
		files, progArgs := args, []string(nil)
		for i, a := range args {
			if a == "--" {
				files, progArgs = args[:i], args[i+1:]
				break
			}
		}
		if len(files) == 0 {
			usage()
		}
		dir, err := os.MkdirTemp("", "golisp-run")
		if err != nil {
			log(err)
		}
		exe := filepath.Join(dir, "main")
		if err := build(goTool(), files, exe, !*goNames); err != nil {
			os.RemoveAll(dir)
			log(err)
		}
		c := exec.Command(exe, progArgs...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		err = c.Run()
		os.RemoveAll(dir)
		if e, ok := err.(*exec.ExitError); ok {
			os.Exit(e.ExitCode())
		}
		if err != nil {
			log(err)
		}

	default:
		usage()
	}
}

// convert converts file (go2lisp or lisp2go) and prints the result to
// stdout, or writes it next to the file if write is set.
func convert(cmd, file string, write bool, stdout, stderr *os.File) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var out []byte
	var ext string
	if cmd == "go2lisp" {
		if n := droppedComments(file, src); n > 0 {
			fmt.Fprintf(stderr, "golisp: %s: warning: %d line directive(s) dropped\n", file, n)
		}
		out, err = syntax.GoToLisp(file, bytes.NewReader(src))
		ext = ".lgo"
	} else {
		out, err = lispToGo(file, src)
		ext = ".go"
	}
	if err != nil {
		return err
	}
	if !write {
		_, err = stdout.Write(out)
		return err
	}
	target := strings.TrimSuffix(file, filepath.Ext(file)) + ext
	return os.WriteFile(target, out, 0o666)
}

// lispToGo converts go-lisp source to gofmt-formatted Go.
func lispToGo(file string, src []byte) ([]byte, error) {
	out, err := syntax.LispToGo(file, bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("%s: formatting generated Go: %v", file, err)
	}
	return formatted, nil
}

// droppedComments counts the comments in Go source that go2lisp drops:
// line directives (//line and /*line*/).
func droppedComments(file string, src []byte) int {
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile(file, -1, len(src)), src, nil, scanner.ScanComments)
	n := 0
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return n
		}
		if tok == token.COMMENT && (strings.HasPrefix(lit, "//line ") || strings.HasPrefix(lit, "/*line ")) {
			n++
		}
	}
}

// goTool returns the go command of the toolchain golisp belongs to.
func goTool() string {
	if root := runtime.GOROOT(); root != "" {
		exe := filepath.Join(root, "bin", "go")
		if runtime.GOOS == "windows" {
			exe += ".exe"
		}
		if _, err := os.Stat(exe); err == nil {
			return exe
		}
	}
	return "go"
}

// build compiles and links the files of a main package into exe.
// If lispNames is set, compiler errors in go-lisp files report
// names with their go-lisp spellings.
func build(goCmd string, files []string, exe string, lispNames bool) error {
	imports, err := importsOf(files)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "golisp-build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	// importcfg with the export data of all (transitive) imports
	args := []string{"list", "-export", "-deps", "-f", "{{if .Export}}packagefile {{.ImportPath}}={{.Export}}{{end}}", "runtime"}
	args = append(args, imports...)
	cfg, err := exec.Command(goCmd, args...).Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("resolving imports: %s", e.Stderr)
		}
		return err
	}
	importcfg := filepath.Join(dir, "importcfg")
	if err := os.WriteFile(importcfg, cfg, 0o666); err != nil {
		return err
	}

	obj := filepath.Join(dir, "main.o")
	for _, cmd := range [][]string{
		append([]string{"tool", "compile", "-p", "main", "-complete", "-importcfg", importcfg, "-o", obj}, files...),
		{"tool", "link", "-importcfg", importcfg, "-o", exe, obj},
	} {
		c := exec.Command(goCmd, cmd...)
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		err := c.Run()
		msgs := out.String()
		if lispNames && cmd[1] == "compile" {
			r := newLispDiagRewriter(files)
			lines := strings.SplitAfter(msgs, "\n")
			for i, l := range lines {
				lines[i] = r.rewrite(l)
			}
			msgs = strings.Join(lines, "")
		}
		os.Stderr.WriteString(msgs)
		if err != nil {
			return fmt.Errorf("%s failed", cmd[1])
		}
	}
	return nil
}

// importsOf returns the import paths of the given Go and go-lisp files,
// which must all belong to package main.
func importsOf(files []string) ([]string, error) {
	seen := map[string]bool{}
	var list []string
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		f, err := syntax.ParserFor(file)(syntax.NewFileBase(file), bytes.NewReader(src), nil, nil, 0)
		if err != nil {
			return nil, err
		}
		if f.PkgName.Value != "main" {
			return nil, fmt.Errorf("%s: package %s is not a main package", file, f.PkgName.Value)
		}
		for _, d := range f.DeclList {
			if imp, ok := d.(*syntax.ImportDecl); ok && imp.Path != nil {
				path, err := strconv.Unquote(imp.Path.Value)
				if err == nil && path != "C" && !seen[path] {
					seen[path] = true
					list = append(list, path)
				}
			}
		}
	}
	return list, nil
}
