package main

import (
	"os"
	"path/filepath"
)

// Directories and files that are expected to be lost and trivially rebuilt.
// Matched against the base name with filepath.Match semantics.
var defaultJunk = []string{
	// JavaScript / TypeScript
	"node_modules", "bower_components", ".pnpm-store", ".yarn-cache",
	".next", ".nuxt", ".turbo", ".parcel-cache", ".svelte-kit", ".angular",
	".nyc_output", ".eslintcache",
	// Python
	"__pycache__", "*.pyc", "*.pyo", ".venv", "venv", ".tox", ".nox",
	".mypy_cache", ".pytest_cache", ".ruff_cache", ".hypothesis",
	".eggs", "*.egg-info", ".ipynb_checkpoints",
	// JVM / Android / Gradle
	".gradle",
	// Swift / Xcode / CocoaPods
	".build", "DerivedData", "Pods",
	// Dart / Flutter
	".dart_tool",
	// Zig
	".zig-cache", "zig-out",
	// Haskell / OCaml / Elixir
	".stack-work", "dist-newstyle", "_build", ".elixir_ls",
	// Infrastructure
	".terraform", ".terragrunt-cache",
	// Editors / language servers / generic caches
	".cache", ".ccls-cache", ".clangd", ".sass-cache",
	// OS droppings
	".DS_Store", "Thumbs.db", "desktop.ini", ".Trash", ".Spotlight-V100",
	".fseventsd", ".TemporaryItems",
	// Environment files
	".env", ".env.*",
}

// contextualJunk is excluded only when a sibling marker file exists, because
// the name alone is too generic (plenty of people have a real "target" dir).
var contextualJunk = map[string][]string{
	"target": {"Cargo.toml"},
	"build":  {"CMakeLists.txt", "build.gradle", "build.gradle.kts", "meson.build"},
	"dist":   {"package.json", "pyproject.toml", "setup.py"},
}

func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// isJunk reports whether an entry named name inside dir should be skipped.
func isJunk(opts *options, dir, name string, isDir bool) bool {
	if matchesAny(opts.excludes, name) {
		return true
	}
	if !opts.skipJunk {
		return false
	}
	if matchesAny(defaultJunk, name) {
		return true
	}
	if isDir {
		if markers, ok := contextualJunk[name]; ok {
			for _, m := range markers {
				if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
					return true
				}
			}
		}
	}
	return false
}
