package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// repo holds everything we need to classify paths inside one git worktree.
type repo struct {
	root         string          // absolute path of the worktree root
	tracked      map[string]bool // slash-separated paths relative to root
	ignoredFiles map[string]bool // individual ignored files
	ignoredDirs  map[string]bool // ignored directories, stored with trailing "/"

	// Repo-level "what would I lose" info (only filled when opts.repoStatus).
	modified int  // tracked files with uncommitted changes (staged or not)
	unpushed int  // commits on local branches that no remote branch contains
	noRemote bool // repo has no remotes configured at all
	commits  int  // total commits, only used when noRemote
}

func gitOutput(root string, args ...string) ([]byte, error) {
	full := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func splitNUL(b []byte) []string {
	var out []string
	for _, p := range bytes.Split(b, []byte{0}) {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

// enclosingRepoRoot returns the worktree root containing dir, or "" if none.
func enclosingRepoRoot(dir string) string {
	out, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root
}

func loadRepo(root string, withStatus bool) (*repo, error) {
	r := &repo{
		root:         root,
		tracked:      map[string]bool{},
		ignoredFiles: map[string]bool{},
		ignoredDirs:  map[string]bool{},
	}

	out, err := gitOutput(root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range splitNUL(out) {
		r.tracked[p] = true
	}

	// --directory collapses fully-ignored directories into a single "dir/" entry,
	// which keeps this fast even with a huge node_modules sitting there.
	out, err = gitOutput(root, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, err
	}
	for _, p := range splitNUL(out) {
		if strings.HasSuffix(p, "/") {
			r.ignoredDirs[p] = true
		} else {
			r.ignoredFiles[p] = true
		}
	}

	if withStatus {
		r.loadStatus()
	}
	return r, nil
}

func (r *repo) loadStatus() {
	if out, err := gitOutput(r.root, "status", "--porcelain", "--untracked-files=no"); err == nil {
		r.modified = countLines(out)
	}

	remotes, err := gitOutput(r.root, "remote")
	if err == nil && strings.TrimSpace(string(remotes)) == "" {
		r.noRemote = true
		if out, err := gitOutput(r.root, "rev-list", "--count", "--all"); err == nil {
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &r.commits)
		}
		return
	}

	// Commits reachable from any local branch but from no remote-tracking branch.
	if out, err := gitOutput(r.root, "rev-list", "--branches", "--not", "--remotes"); err == nil {
		r.unpushed = countLines(out)
	}
}

func countLines(b []byte) int {
	n := 0
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) > 0 {
			n++
		}
	}
	return n
}
