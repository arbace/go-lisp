// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tool

// lispTools are the go-lisp tools that are not cmd/<name>: golisp lives under
// cmd/compile, where it may import cmd/compile/internal/syntax, so `go tool
// golisp` must be told its package to build it as it builds the other tools
// make.bash leaves unbuilt.
var lispTools = map[string]string{
	"golisp": "cmd/compile/golisp",
}
