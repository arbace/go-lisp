" Vim indent file
" Language: go-lisp (golisp/SPEC.md)
"
" The indentation of the go-lisp printer (go tool golisp go2lisp):
" the bodies of special forms such as func, if, for, case, struct, and
" :lit are indented by 'shiftwidth' relative to the line on which the
" form starts; other elements align with the first argument of the
" enclosing list, or with the first element of a vector or map.

if exists("b:did_indent")
  finish
endif
let b:did_indent = 1

setlocal nolisp
setlocal autoindent
" Keep the whitespace of lines whose indentation does not change, such
" as lines inside multi-line raw strings, where it is part of the value.
setlocal preserveindent
setlocal indentexpr=GolispIndent()
setlocal indentkeys=!^F,o,O,0),0],0}

let b:undo_indent = "setlocal lisp< autoindent< preserveindent< indentexpr< indentkeys<"

if exists("*GolispIndent")
  finish
endif

let s:cpo_save = &cpo
set cpo&vim

" Forms whose remaining elements are bodies (statements, specs, fields,
" clauses, elements) rather than arguments.
let s:body_forms = {}
for s:f in ['package', 'import', 'const', 'var', 'type', 'func', 'struct',
      \ 'interface', 'if', 'else', 'for', 'switch', 'select', 'case',
      \ 'default', 'go', 'defer', ':block', ':label', ':lit', ':type-switch']
  let s:body_forms[s:f] = 1
endfor

" s:Enclosing returns the innermost bracket that is open at the start of
" line lnum as [line, column, char], [] if there is none, or 0 if the
" line continues a multi-line raw string. It scans forward from the
" start of the enclosing top-level form (a line starting with "(" in
" column 1), skipping strings, runes, raw strings, and comments.
" When lines are indented one after the other (as by =), the scan of
" the previous call is continued instead of started over.
function! s:Enclosing(lnum) abort
  let c = get(b:, 'golisp_indent_cache', {})
  if !empty(c) && c.lnum == a:lnum - 1 && b:changedtick <= c.tick + 1
    let stack = copy(c.stack)
    let state = c.state
    let start = a:lnum - 1
  else
    let start = a:lnum - 1
    while start > 1 && getline(start) !~# '^('
      let start -= 1
    endwhile
    let start = max([start, 1])
    let stack = []
    let state = ''
  endif
  for l in range(start, a:lnum - 1)
    if l < 1
      continue
    endif
    let [stack, state] = s:Scan(l, stack, state)
  endfor
  let b:golisp_indent_cache = {'lnum': a:lnum, 'tick': b:changedtick, 'stack': copy(stack), 'state': state}
  if state ==# '`'
    return 0
  endif
  return empty(stack) ? [] : stack[-1]
endfunction

" s:Scan scans line l, given the brackets open and the literal being
" read at its start, and returns them for the end of the line.
function! s:Scan(l, stack, state) abort
  let stack = a:stack
  let state = a:state
  let line = getline(a:l)
  let n = strlen(line)
  let i = 0
  while i < n
    let c = line[i]
    if state ==# ''
      if c ==# ';'
        break
      elseif c ==# '"' || c ==# "'" || c ==# '`'
        let state = c
      elseif c ==# '(' || c ==# '[' || c ==# '{'
        call add(stack, [a:l, i + 1, c])
      elseif (c ==# ')' || c ==# ']' || c ==# '}') && !empty(stack)
        call remove(stack, -1)
      endif
    elseif c ==# '\' && state !=# '`'
      let i += 1
    elseif c ==# state
      let state = ''
    endif
    let i += 1
  endwhile
  if state !=# '`'
    let state = '' " only raw strings span lines
  endif
  return [stack, state]
endfunction

function! GolispIndent() abort
  let open = s:Enclosing(v:lnum)
  if type(open) == v:t_number
    " Inside a raw string: keep the line as it is (with
    " 'preserveindent', an unchanged indent keeps its characters).
    return indent(v:lnum)
  endif
  if empty(open)
    return 0
  endif
  let [pl, pc, bracket] = open
  let line = getline(pl)
  if bracket !=# '('
    " A vector or map: align with its first element.
    return pc
  endif
  let head = matchstr(line, '^[^[:space:]()[\]{}";]\+', pc)
  if head ==# '' || has_key(s:body_forms, head)
    return indent(pl) + shiftwidth()
  endif
  " A call or other form: align with its first argument, if there is
  " one on the same line.
  let arg = match(line, '\S', pc + strlen(head))
  if arg >= 0 && line[arg] !~# '[;)]'
    return arg
  endif
  return pc + 1
endfunction

let &cpo = s:cpo_save
unlet s:cpo_save
