// repoless finds files under a directory that would be lost if the disk died:
// files not inside any git repo, files a repo ignores, and files a repo has
// never had added. It also flags repos with uncommitted or unpushed work.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const version = "0.1.0"

type options struct {
	skipJunk   bool
	excludes   []string
	verbose    bool
	duDepth    int // du -d style listing depth, -1 when off
	minDepth   int
	maxDepth   int
	repoStatus bool
	only       [nStatus]bool
	json       bool
	color      bool
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func usage() {
	fmt.Fprintf(os.Stderr, `repoless %s - find files that are not safely in a git repo

Usage: repoless [flags] [directory]

Scans directory (default ".") and reports every file that would be lost if
the drive died: files outside any repo, files a repo ignores, and files a
repo has never had added. Repos with uncommitted or unpushed work are listed
too. Requires git on PATH.

Statuses:
  no-repo     not inside any git repository
  ignored     inside a repo but excluded by .gitignore rules
  untracked   inside a repo, not ignored, but never added

Flags:
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
      --no-color         disable colored output
      --version          print version
  -h, --help             this help

Examples:
  repoless -j ~/projects
  repoless -j -d 1 ~/code              # what each project would lose
  repoless -j --only no-repo,untracked ~/code
  repoless -v -j --json . | jq '.entries[].path'
`, version)
}

func main() {
	o := &options{}
	var excludes multiFlag
	var only string
	var noRepoStatus, noColor, showVersion bool

	fs := flag.NewFlagSet("repoless", flag.ExitOnError)
	fs.Usage = usage
	fs.BoolVar(&o.skipJunk, "j", false, "")
	fs.BoolVar(&o.skipJunk, "skip-junk", false, "")
	fs.Var(&excludes, "x", "")
	fs.Var(&excludes, "exclude", "")
	fs.BoolVar(&o.verbose, "v", false, "")
	fs.BoolVar(&o.verbose, "verbose", false, "")
	fs.IntVar(&o.duDepth, "d", -1, "")
	fs.IntVar(&o.duDepth, "depth", -1, "")
	fs.IntVar(&o.minDepth, "min-depth", 1, "")
	fs.IntVar(&o.maxDepth, "max-depth", 0, "")
	fs.StringVar(&only, "only", "", "")
	fs.BoolVar(&noRepoStatus, "no-repo-status", false, "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.BoolVar(&noColor, "no-color", false, "")
	fs.BoolVar(&showVersion, "version", false, "")
	fs.Parse(os.Args[1:])

	if showVersion {
		fmt.Println("repoless", version)
		return
	}
	if fs.NArg() > 1 {
		fatal("expected at most one directory argument")
	}
	if _, err := exec.LookPath("git"); err != nil {
		fatal("git not found on PATH")
	}

	o.excludes = excludes
	o.repoStatus = !noRepoStatus
	o.color = !noColor && !o.json && isTerminal(os.Stdout) && os.Getenv("NO_COLOR") == ""
	for st := stNoRepo; st < nStatus; st++ {
		o.only[st] = only == ""
	}
	if only != "" {
		for _, name := range strings.Split(only, ",") {
			name = strings.TrimSpace(name)
			found := false
			for st := stNoRepo; st < nStatus; st++ {
				if st.String() == name {
					o.only[st], found = true, true
				}
			}
			if !found {
				fatal("unknown status %q for --only (want no-repo, ignored, untracked)", name)
			}
		}
	}
	for _, p := range o.excludes {
		if _, err := filepath.Match(p, ""); err != nil {
			fatal("bad --exclude pattern %q: %v", p, err)
		}
	}

	target := "."
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		fatal("%v", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		fatal("%v", err)
	}
	if !info.IsDir() {
		fatal("%s is not a directory", target)
	}

	displayBase := filepath.Clean(target)
	if displayBase == "." {
		displayBase = ""
	}

	s := newScanner(o)
	root := s.scan(abs)
	rep := buildReport(o, s, root, displayBase, abs)

	if o.json {
		if err := writeJSON(os.Stdout, rep); err != nil {
			fatal("%v", err)
		}
	} else {
		writeText(os.Stdout, o, rep, root)
	}
	for _, w := range s.warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "repoless: "+format+"\n", args...)
	os.Exit(2)
}
