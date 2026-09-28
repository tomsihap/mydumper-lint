package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// readTreeDir loads a source tree from an extracted archive (development aid
// for -debug-dir).
func readTreeDir(dir string) (srcTree, error) {
	tree := srcTree{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() || !keepPath(rel) {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // G122: p comes from filepath.WalkDir over -debug-dir, a developer-supplied local path
		if err != nil {
			return err
		}
		tree[rel] = b
		return nil
	})
	return tree, err
}

// keepPath selects the archive members the extractor reads.
func keepPath(rel string) bool {
	if rel == "CMakeLists.txt" {
		return true
	}
	return strings.HasPrefix(rel, "src/") && (strings.HasSuffix(rel, ".c") || strings.HasSuffix(rel, ".h"))
}

// dumpExtraction prints what the extractor found in one tag.
func dumpExtraction(w io.Writer, x *extraction) {
	fmt.Fprintf(w, "tag %s\n", x.tag)
	fmt.Fprintf(w, "  ignore_unknown_options %v\n", x.ignoreUnknown)
	fmt.Fprintf(w, "  loader_fingerprint %s\n", x.fingerprint)
	fmt.Fprintf(w, "  products %s\n", strings.Join(x.products, " "))
	fmt.Fprintf(w, "  product option groups read by %s\n", strings.Join(x.productGroups, " "))
	fmt.Fprintf(w, "  table sections lost by %s\n", strings.Join(x.tablesLost, " "))
	fmt.Fprintf(w, "  table keys %s\n", factList(x.tableKeys))
	fmt.Fprintf(w, "  masking functions %s\n", factList(x.masquerade))
	for _, n := range x.notes {
		fmt.Fprintf(w, "  note: %s\n", n)
	}
	for _, tool := range tools {
		fmt.Fprintf(w, "  %s: %d options\n", tool, len(x.options[tool]))
		for _, name := range sortedKeys(x.options[tool]) {
			for _, v := range x.options[tool][name] {
				fmt.Fprintf(w, "    %-40s short=%-1s arg=%-8s flags=%s group=%q cond=%q %s\n",
					name, v.def.Short, v.def.Arg, strings.Join(v.def.Flags, ","), v.def.Group, v.cond, v.source)
			}
		}
	}
}

func factList(fs []factName) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		if f.prefix {
			parts = append(parts, f.name+"*")
		} else {
			parts = append(parts, f.name)
		}
	}
	return strings.Join(parts, " ")
}
