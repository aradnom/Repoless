package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// entry is one output line: a file, a uniformly at-risk directory, or a mixed
// directory summarised because of --max-depth.
type entry struct {
	Path   string         `json:"path"`
	Status string         `json:"status"` // no-repo, ignored, untracked, or mixed (several)
	IsDir  bool           `json:"is_dir"`
	Files  int            `json:"files"`
	Bytes  int64          `json:"bytes"`
	Detail map[string]int `json:"detail,omitempty"` // per-status counts when the dir is only partly at risk
}

type repoReport struct {
	Path     string `json:"path"`
	Modified int    `json:"modified_uncommitted"`
	Unpushed int    `json:"commits_not_on_remote"`
	NoRemote bool   `json:"no_remote"`
	Commits  int    `json:"commits,omitempty"`
}

type report struct {
	Target    string         `json:"target"`
	Entries   []entry        `json:"entries"`
	Repos     []repoReport   `json:"repos"`
	Totals    map[string]any `json:"totals"`
	Junk      map[string]int `json:"junk_skipped,omitempty"`
	BareRepos int            `json:"bare_repos_skipped,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
}

// visible reports how many of n's files are at-risk and selected by --only,
// the status they share if all of n's files share one, and whether they do.
func (o *options) visible(n *node) (shown int, st status, uniform bool) {
	hidden := n.counts[stSafe]
	for s := stNoRepo; s < nStatus; s++ {
		if n.counts[s] == 0 {
			continue
		}
		if o.only[s] {
			shown += n.counts[s]
			st = s
		} else {
			hidden += n.counts[s]
		}
	}
	uniform = shown > 0 && hidden == 0 && shown == n.counts[st]
	return
}

// summarise turns a node into a single entry: a file, a uniform directory,
// or a mixed directory carrying a per-status breakdown.
func (o *options) summarise(n *node) entry {
	if !n.isDir {
		return entry{Path: n.rel, Status: n.st.String(), Files: 1, Bytes: n.size}
	}
	if _, st, uniform := o.visible(n); uniform {
		return entry{Path: n.rel, Status: st.String(), IsDir: true, Files: n.counts[st], Bytes: n.sizes[st]}
	}
	// Partially at risk: Detail carries the per-status breakdown. The status
	// is "mixed" only when more than one at-risk status is present.
	e := entry{Path: n.rel, Status: "mixed", IsDir: true, Detail: map[string]int{}}
	for st := stNoRepo; st < nStatus; st++ {
		if o.only[st] && n.counts[st] > 0 {
			e.Detail[st.String()] = n.counts[st]
			e.Files += n.counts[st]
			e.Bytes += n.sizes[st]
			if len(e.Detail) == 1 {
				e.Status = st.String()
			} else {
				e.Status = "mixed"
			}
		}
	}
	return e
}

// collect flattens the node tree into output entries following the
// collapse rules controlled by --min-depth, --max-depth and --verbose.
func (o *options) collect(n *node, depth int, out *[]entry) {
	shown, _, uniform := o.visible(n)
	if shown == 0 {
		return
	}
	if !n.isDir {
		*out = append(*out, o.summarise(n))
		return
	}
	if uniform && !o.verbose && depth >= o.minDepth {
		*out = append(*out, o.summarise(n))
		return
	}
	if o.maxDepth > 0 && depth >= o.maxDepth && !o.verbose {
		*out = append(*out, o.summarise(n))
		return
	}
	for _, c := range n.children {
		o.collect(c, depth+1, out)
	}
}

// collectDepth is the du -d style listing: every directory and loose file at
// or above the given depth gets one summary line, children before parents.
func (o *options) collectDepth(n *node, depth int, out *[]entry) {
	if shown, _, _ := o.visible(n); shown == 0 {
		return
	}
	if n.isDir && depth < o.duDepth {
		for _, c := range n.children {
			o.collectDepth(c, depth+1, out)
		}
	}
	if depth == 0 && o.duDepth > 0 {
		return // the root total is already the Summary
	}
	*out = append(*out, o.summarise(n))
}

func buildReport(o *options, s *scanner, root *node, displayBase, absTarget string) report {
	var entries []entry
	if o.duDepth >= 0 {
		o.collectDepth(root, 0, &entries)
	} else {
		o.collect(root, 0, &entries)
	}
	for i := range entries {
		entries[i].Path = displayPath(displayBase, entries[i].Path, entries[i].IsDir)
	}

	var repos []repoReport
	for _, r := range s.repos {
		if r.modified == 0 && r.unpushed == 0 && !r.noRemote {
			continue
		}
		p := r.root + string(filepath.Separator)
		if rel, err := filepath.Rel(absTarget, r.root); err == nil && !strings.HasPrefix(rel, "..") {
			p = displayPath(displayBase, filepath.ToSlash(rel), true)
		}
		repos = append(repos, repoReport{Path: p, Modified: r.modified, Unpushed: r.unpushed, NoRemote: r.noRemote, Commits: r.commits})
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Path < repos[j].Path })

	totals := map[string]any{}
	var tf int
	var tb int64
	for st := stNoRepo; st < nStatus; st++ {
		if !o.only[st] {
			continue
		}
		totals[st.String()] = map[string]any{"files": root.counts[st], "bytes": root.sizes[st]}
		tf += root.counts[st]
		tb += root.sizes[st]
	}
	totals["files"] = tf
	totals["bytes"] = tb
	totals["repos_scanned"] = len(s.repos)

	rep := report{Target: absTarget, Entries: entries, Repos: repos, Totals: totals, Warnings: s.warnings, BareRepos: s.bareRepos}
	if len(s.junk) > 0 {
		rep.Junk = s.junk
	}
	return rep
}

func displayPath(base, rel string, isDir bool) string {
	var p string
	switch {
	case rel == "" && base == "":
		p = "."
	case rel == "":
		p = base
	case base == "":
		p = filepath.FromSlash(rel)
	default:
		p = filepath.Join(base, filepath.FromSlash(rel))
	}
	if isDir && !strings.HasSuffix(p, string(filepath.Separator)) {
		p += string(filepath.Separator)
	}
	return p
}

func writeJSON(w io.Writer, rep report) error {
	if rep.Entries == nil {
		rep.Entries = []entry{}
	}
	if rep.Repos == nil {
		rep.Repos = []repoReport{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

const (
	cReset  = "\033[0m"
	cDim    = "\033[2m"
	cBold   = "\033[1m"
	cRed    = "\033[31m"
	cYellow = "\033[33m"
	cMag    = "\033[35m"
	cCyan   = "\033[36m"
)

func (o *options) paint(code, s string) string {
	if !o.color {
		return s
	}
	return code + s + cReset
}

func statusColor(st string) string {
	switch st {
	case "no-repo":
		return cRed
	case "ignored":
		return cYellow
	case "untracked":
		return cMag
	default:
		return cCyan
	}
}

func writeText(w io.Writer, o *options, rep report, root *node) {
	countW := 0
	for _, e := range rep.Entries {
		if l := len(entryCount(e)); l > countW {
			countW = l
		}
	}

	for _, e := range rep.Entries {
		tag := o.paint(statusColor(e.Status), fmt.Sprintf("%-9s", e.Status))
		fmt.Fprintf(w, "%s  %*s  %9s  %s\n", tag, countW, entryCount(e), humanBytes(e.Bytes), e.Path)
	}
	if len(rep.Entries) > 0 {
		fmt.Fprintln(w)
	}

	if len(rep.Repos) > 0 {
		fmt.Fprintln(w, o.paint(cBold, "Repos with uncommitted or unpushed work"))
		for _, r := range rep.Repos {
			var parts []string
			if r.Modified > 0 {
				parts = append(parts, fmt.Sprintf("%d modified %s not committed", r.Modified, plural(r.Modified, "file", "files")))
			}
			if r.NoRemote {
				parts = append(parts, fmt.Sprintf("no remote configured (%d %s)", r.Commits, plural(r.Commits, "commit", "commits")))
			} else if r.Unpushed > 0 {
				parts = append(parts, fmt.Sprintf("%d %s not on any remote", r.Unpushed, plural(r.Unpushed, "commit", "commits")))
			}
			fmt.Fprintf(w, "  %s\n      %s\n", r.Path, o.paint(cDim, strings.Join(parts, ", ")))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, o.paint(cBold, "Summary"))
	var tf int
	var tb int64
	for st := stNoRepo; st < nStatus; st++ {
		if !o.only[st] {
			continue
		}
		fmt.Fprintf(w, "  %s  %6d %-5s  %9s\n", o.paint(statusColor(st.String()), fmt.Sprintf("%-9s", st.String())),
			root.counts[st], plural(root.counts[st], "file", "files"), humanBytes(root.sizes[st]))
		tf += root.counts[st]
		tb += root.sizes[st]
	}
	fmt.Fprintf(w, "  %-9s  %6d %-5s  %9s\n", "total", tf, plural(tf, "file", "files"), humanBytes(tb))
	scanned := rep.Totals["repos_scanned"].(int)
	note := fmt.Sprintf("%d %s scanned", scanned, plural(scanned, "repo", "repos"))
	if rep.BareRepos > 0 {
		note += fmt.Sprintf(", %d bare %s skipped", rep.BareRepos, plural(rep.BareRepos, "repo", "repos"))
	}
	fmt.Fprintf(w, "  %s\n", o.paint(cDim, note))
	if len(rep.Junk) > 0 {
		fmt.Fprintf(w, "  %s\n", o.paint(cDim, "junk skipped: "+junkSummary(rep.Junk)))
	}
}

func entryCount(e entry) string {
	if !e.IsDir {
		return ""
	}
	if e.Detail != nil {
		return mixedDetail(e.Detail)
	}
	return fmt.Sprintf("%d %s", e.Files, plural(e.Files, "file", "files"))
}

func mixedDetail(d map[string]int) string {
	var parts []string
	for st := stNoRepo; st < nStatus; st++ {
		if c, ok := d[st.String()]; ok {
			parts = append(parts, fmt.Sprintf("%d %s", c, st))
		}
	}
	return strings.Join(parts, ", ")
}

func junkSummary(j map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	total := 0
	for k, v := range j {
		kvs = append(kvs, kv{k, v})
		total += v
	}
	sort.Slice(kvs, func(a, b int) bool {
		if kvs[a].v != kvs[b].v {
			return kvs[a].v > kvs[b].v
		}
		return kvs[a].k < kvs[b].k
	})
	var parts []string
	for i, e := range kvs {
		if i == 5 {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, fmt.Sprintf("%s x%d", e.k, e.v))
	}
	return fmt.Sprintf("%d (%s)", total, strings.Join(parts, ", "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
