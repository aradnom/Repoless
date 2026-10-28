# repoless

A tool for finding files that aren't in a repo (but maybe should be).

Finds the files under a directory that would be gone if your drive imploded:
anything that is not safely committed in a git repo.

```
$ repoless -j ~/code
no-repo       142 files    3.2 MB  /home/alice/code/notes/
ignored                    120 B   /home/alice/code/api/.env
untracked    3 files      10.1 KB  /home/alice/code/api/scratch/
ignored     7360 files    7.3 GB   /home/alice/code/ml/data/

Repos with uncommitted or unpushed work
  /home/alice/code/api/
      2 modified files not committed, 5 commits not on any remote
  /home/alice/code/experiment/
      no remote configured (12 commits)

Summary
  no-repo       142 files     3.2 MB
  ignored      7361 files     7.3 GB
  untracked       3 files    10.1 KB
  total        7506 files     7.3 GB
  12 repos scanned
  junk skipped: 40 (.DS_Store x30, node_modules x8, __pycache__ x2)
```

Every file gets one of three statuses:

| status      | meaning                                                     |
|-------------|-------------------------------------------------------------|
| `no-repo`   | not inside any git repository                               |
| `ignored`   | inside a repo but excluded by `.gitignore` rules            |
| `untracked` | inside a repo, not ignored, but never `git add`ed           |

Tracked files are considered safe and not listed. Separately, every repo that
was scanned is checked for uncommitted modifications to tracked files, commits
that exist on no remote branch, and the absence of any remote at all.

Classification is delegated to the `git` binary (`git ls-files`), so it agrees
exactly with what git thinks: nested `.gitignore` files, global excludes,
`.git/info/exclude`, worktrees, submodules and nested repos all behave as git
would treat them. A nested repo takes precedence over the enclosing one.

## Install

Requires Go 1.26+ to build and `git` on `PATH` to run.

```
make build            # ./repoless
make install          # into $GOPATH/bin
make release          # dist/ binaries for darwin and linux, amd64 and arm64
```

## Usage

```
repoless [flags] [directory]      # directory defaults to "."

  -j, --skip-junk        skip rebuildable junk (node_modules, __pycache__, .venv,
                         .DS_Store, Cargo target/, ...)
  -x, --exclude PATTERN  skip entries whose name matches PATTERN (repeatable,
                         glob syntax, applies with or without -j)
  -d, --depth N          du-style: one total per directory (and loose file) at
                         depth N or shallower, children before parents
  -v, --verbose          list every file instead of collapsing directories
      --min-depth N      always expand directories at least N levels (default 1)
      --max-depth N      never expand deeper than N levels; summarise instead
      --only LIST        comma-separated statuses to report (default all three)
      --no-repo-status   skip the per-repo uncommitted/unpushed checks
      --json             machine-readable output
      --no-color         disable colored output (NO_COLOR is also honoured)
```

### Output modes

By default a directory whose contents all share one at-risk status is printed
as a single line with a file count and total size, rather than one line per
file, and mixed directories are expanded. `--min-depth` forces expansion near
the top so the target itself never collapses into one line; `--max-depth` caps
expansion; `-v` lists every file.

`-d N` works like `du -d N`: every directory at depth N or shallower gets one
line with its at-risk file count and size, children listed before parents.
Directories with nothing at risk are omitted. A directory that also holds
safe (tracked) files shows a per-status breakdown in place of the file count,
and is labelled `mixed` when more than one at-risk status is present.

```
$ repoless -j -d 1 ~/code
no-repo                     30 files     3.5 GB  /home/alice/code/archive/
no-repo                                  1.2 GB  /home/alice/code/old-backup.tgz
ignored                  12 ignored     5.1 MB  /home/alice/code/api/
mixed      2017 ignored, 3 untracked   105.3 MB  /home/alice/code/webapp/
```

### Junk

`-j` skips directories and files that are expected to be lost and trivially
rebuilt: dependency caches (`node_modules`, `.venv`, `Pods`, `.gradle`, ...),
tool caches (`__pycache__`, `.mypy_cache`, `.terraform`, ...) and OS droppings
(`.DS_Store`, `Thumbs.db`). Generic names are only treated as junk when a
marker file sits beside them: `target/` next to `Cargo.toml`, `build/` next to
`CMakeLists.txt` or `build.gradle`, `dist/` next to `package.json` or
`pyproject.toml`. See `junk.go` for the full list; add your own with `-x`.

### Exit status

0 on success, 2 on usage errors. Unreadable directories are reported as
warnings on stderr and skipped. Bare repositories are skipped and counted.
