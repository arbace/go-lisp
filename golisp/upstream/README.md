# Fixes for upstream Go

Two bugs in Go's compiler front end (`cmd/compile/internal/syntax`),
found while building go-lisp, reported upstream, with fixes ready to send.

| Patch | Issue | Bug |
|---|---|---|
| `0001-syntax-report-NUL-bytes-after-a-buffer-refill.patch` | [#81632](https://github.com/golang/go/issues/81632) | The compiler accepts a NUL byte in source if it is the first byte after the scanner's buffer is filled or grown (for example at offset 8190 of a string literal), and compiles it into the program. |
| `0002-syntax-print-empty-declaration-statements.patch` | [#81633](https://github.com/golang/go/issues/81633) | The syntax printer panics on an empty declaration statement (`var ()` in a function body). |

About the patches:
- **Base:** each is based on Go's `master`, touches different files, and
  applies on its own, so they can be two independent changes.
- **Tests:** each has a regression test that fails without the fix and
  passes with it. The whole `cmd/compile/internal/syntax` package passes.

## Submitting

Go takes changes through Gerrit, not GitHub pull requests. See
https://go.dev/doc/contribute.

1. **Sign the CLA.** Sign the Google Individual Contributor License
   Agreement at https://cla.developers.google.com/, with the Google
   account you will use for Gerrit. Its email should match the author of
   the patches (currently `rhz <robert.zawiasa@gmail.com>`; change the
   `From:` line if needed).
2. **Set up Gerrit.**
   - Get credentials at https://go.googlesource.com/new-password.
   - Sign in once at https://go-review.googlesource.com/login/.
   - Install the tool:
     `go install golang.org/x/review/git-codereview@latest`
3. **Send each patch** from a clone of upstream Go:

   ```sh
   git clone https://go.googlesource.com/go && cd go
   git codereview hooks                    # adds Change-Id lines to commits
   git checkout -b issue81632 origin/master
   git am /path/to/0001-syntax-report-NUL-bytes-after-a-buffer-refill.patch
   git commit --amend --no-edit            # let the hook add a Change-Id
   git codereview mail                     # sends the change to Gerrit

   git checkout -b issue81633 origin/master
   git am /path/to/0002-syntax-print-empty-declaration-statements.patch
   git commit --amend --no-edit
   git codereview mail
   ```

The commit messages follow Go's conventions and end with `Fixes #NNNNN`,
which links each change to its issue. They also carry a `Co-Authored-By:
Claude` trailer. Check Go's current contribution policy on AI-assisted
changes, and remove the trailer if needed.
