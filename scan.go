package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type status int

const (
	stSafe status = iota
	stNoRepo
	stIgnored
	stUntracked
	nStatus
)

var statusNames = [nStatus]string{"safe", "no-repo", "ignored", "untracked"}

func (s status) String() string { return statusNames[s] }

// node is one file or directory in the scanned tree with per-status totals.
type node struct {
	rel      string // slash-separated path relative to the target, "" for root
	isDir    bool
	st       status // files only
	size     int64  // files only
	counts   [nStatus]int
	sizes    [nStatus]int64
	children []*node
}

func (n *node) add(child *node) {
	n.children = append(n.children, child)
	for i := range n.counts {
		n.counts[i] += child.counts[i]
		n.sizes[i] += child.sizes[i]
	}
}

type scanner struct {
	opts      *options
	repos     []*repo
	repoByDir map[string]*repo
	junk      map[string]int // base name -> occurrences skipped
	junkCount int
	bareRepos int
	warnings  []string
}

func newScanner(opts *options) *scanner {
	return &scanner{opts: opts, repoByDir: map[string]*repo{}, junk: map[string]int{}}
}

func (s *scanner) warn(format string, args ...any) {
	s.warnings = append(s.warnings, fmt.Sprintf(format, args...))
}

func (s *scanner) repoFor(root string) *repo {
	if r, ok := s.repoByDir[root]; ok {
		return r
	}
	r, err := loadRepo(root, s.opts.repoStatus)
	if err != nil {
		s.warn("%s: %v", root, err)
		s.repoByDir[root] = nil
		return nil
	}
	s.repoByDir[root] = r
	s.repos = append(s.repos, r)
	return r
}

// scan walks target (absolute, symlinks resolved) and returns the root node.
func (s *scanner) scan(target string) *node {
	var r *repo
	if root := enclosingRepoRoot(target); root != "" {
		r = s.repoFor(root)
	}
	repoRel := ""
	inIgnored := false
	if r != nil {
		if rel, err := filepath.Rel(r.root, target); err == nil && rel != "." {
			repoRel = filepath.ToSlash(rel)
			inIgnored = r.underIgnoredDir(repoRel)
		}
	}
	return s.walk(target, "", r, repoRel, inIgnored)
}

func (r *repo) underIgnoredDir(repoRel string) bool {
	for i := 0; i < len(repoRel); i++ {
		if repoRel[i] == '/' && r.ignoredDirs[repoRel[:i+1]] {
			return true
		}
	}
	return r.ignoredDirs[repoRel+"/"]
}

func (r *repo) classify(repoRel string, inIgnored bool) status {
	if r == nil {
		return stNoRepo
	}
	if r.tracked[repoRel] {
		return stSafe
	}
	if inIgnored || r.ignoredFiles[repoRel] {
		return stIgnored
	}
	return stUntracked
}

func (s *scanner) walk(abs, rel string, r *repo, repoRel string, inIgnored bool) *node {
	n := &node{rel: rel, isDir: true}

	entries, err := os.ReadDir(abs)
	if err != nil {
		s.warn("%v", err)
		return n
	}

	// A bare repository has no worktree to classify; its objects are the repo.
	if isBareRepo(entries) {
		s.bareRepos++
		return n
	}

	// A directory containing .git (dir, or file for worktrees/submodules) is a
	// new repo context, which takes precedence over any enclosing repo's view.
	if hasGit(entries) && (r == nil || r.root != abs) {
		if nr := s.repoFor(abs); nr != nil {
			r, repoRel, inIgnored = nr, "", false
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
			continue
		}
		if isJunk(s.opts, abs, name, e.IsDir()) {
			s.junk[name]++
			s.junkCount++
			continue
		}

		childAbs := filepath.Join(abs, name)
		childRel := joinRel(rel, name)
		childRepoRel := joinRel(repoRel, name)

		if e.IsDir() {
			childIgnored := inIgnored || (r != nil && r.ignoredDirs[childRepoRel+"/"])
			n.add(s.walk(childAbs, childRel, r, childRepoRel, childIgnored))
			continue
		}

		info, err := e.Info()
		if err != nil {
			s.warn("%v", err)
			continue
		}
		st := r.classify(childRepoRel, inIgnored)
		child := &node{rel: childRel, st: st, size: info.Size()}
		child.counts[st] = 1
		child.sizes[st] = info.Size()
		n.add(child)
	}
	return n
}

func hasGit(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.Name() == ".git" {
			return true
		}
	}
	return false
}

// isBareRepo recognises the layout of a bare repository (HEAD, objects/,
// refs/, config) so we neither descend into it nor report it as loose files.
func isBareRepo(entries []os.DirEntry) bool {
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Name()] = true
	}
	return seen["HEAD"] && seen["objects"] && seen["refs"] && seen["config"] && !seen[".git"]
}

func joinRel(base, name string) string {
	if base == "" {
		return name
	}
	return base + "/" + name
}
