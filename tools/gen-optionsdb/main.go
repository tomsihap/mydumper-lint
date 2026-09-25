// Command gen-optionsdb generates internal/optionsdb/data/optionsdb.json, the
// knowledge base of mydumper and myloader facts per version (design §5).
//
// It lists the upstream tags and GitHub releases, downloads each source
// archive once into a cache, extracts facts with a small C tokenizer, merges
// them into version ranges, applies a hand-maintained overlay, and writes
// sorted, deterministic JSON. With -verify-images it also cross-checks every
// version against the official Docker image (design §5.4).
//
// mydumper is GPL-3.0: the generator reads the source to learn facts (names,
// types, flags, conditions) and never copies code, comments or help texts.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	os.Exit(runMain(os.Args[1:]))
}

func runMain(args []string) int {
	fl := flag.NewFlagSet("gen-optionsdb", flag.ContinueOnError)
	debugDir := fl.String("debug-dir", "", "analyse an extracted source tree and print its facts (development aid)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *debugDir != "" {
		tree, err := readTreeDir(*debugDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gen-optionsdb:", err)
			return 1
		}
		x, err := extract(*debugDir, tree)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gen-optionsdb:", err)
			return 1
		}
		dumpExtraction(os.Stdout, x)
		return 0
	}
	fmt.Fprintln(os.Stderr, "gen-optionsdb: not implemented yet")
	return 2
}
