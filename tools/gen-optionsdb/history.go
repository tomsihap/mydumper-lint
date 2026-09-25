package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
)

// funcPrint is the token fingerprint of one function definition in one
// version, computed like the loader fingerprint.
type funcPrint struct {
	path, hash string
}

// functionPrints returns the fingerprint of every definition of each name in
// a source tree, for the official build.
func functionPrints(tree srcTree, names []string) (map[string][]funcPrint, error) {
	cus := map[string]*cfgUnit{}
	cfg := allConfigs()[officialConfigIndex()]
	for _, p := range sortedKeys(tree) {
		if !strings.HasPrefix(p, "src/") || !strings.HasSuffix(p, ".c") {
			continue
		}
		u, err := newUnit(p, tree[p])
		if err != nil {
			return nil, err
		}
		cu := filterUnit(u, cfg)
		if err := cu.parse(); err != nil {
			return nil, err
		}
		cus[p] = cu
	}
	out := map[string][]funcPrint{}
	for _, name := range names {
		for _, f := range findFuncs(cus, name) {
			var sb strings.Builder
			from, to := f.cu.rawIdx[f.nameIdx], f.cu.rawIdx[f.close]
			for k, t := range f.cu.u.raw[from : to+1] {
				if k > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(t.text)
			}
			sum := sha256.Sum256([]byte(sb.String()))
			out[name] = append(out[name], funcPrint{path: f.cu.u.path, hash: hex.EncodeToString(sum[:])[:12]})
		}
	}
	return out, nil
}

// printFuncHistory prints, for each function, the version ranges over which
// its definition is token-identical: the evidence ranges of overlay entries.
func printFuncHistory(w io.Writer, names []string, tags []string, prints []map[string][]funcPrint) {
	for _, name := range names {
		fmt.Fprintf(w, "%s:\n", name)
		start := 0
		for i := 1; i <= len(tags); i++ {
			if i < len(tags) && describePrints(prints[i][name]) == describePrints(prints[start][name]) {
				continue
			}
			to := tags[i-1]
			fmt.Fprintf(w, "  %s..%s %s\n", tags[start], to, describePrints(prints[start][name]))
			start = i
		}
	}
}

func describePrints(ps []funcPrint) string {
	if len(ps) == 0 {
		return "(absent)"
	}
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		parts = append(parts, p.path+"@"+p.hash)
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
