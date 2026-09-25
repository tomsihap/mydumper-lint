// Command gen-optionsdb generates internal/optionsdb/data/optionsdb.json, the
// knowledge base of mydumper and myloader facts per version (design §5).
//
// It lists the upstream tags and GitHub releases, downloads each source
// archive once into a cache, extracts facts with a small C tokenizer, merges
// them into version ranges, applies the hand-maintained overlay.json, and
// writes sorted, deterministic JSON. With -verify-images it also cross-checks
// every version against the official Docker image (design §5.4) and records
// the results in images.json.
//
// mydumper is GPL-3.0: the generator reads the source to learn facts (names,
// types, flags, conditions) and never copies code, comments or help texts.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

type options struct {
	cache, out, overlay, images string
	offline, verify, check      bool
	docker                      string
	jobs                        int
	minTag                      string
	debugDir                    string
	exportSrc                   string
	funcHistory                 string
}

func runMain(args []string, stdout, stderr io.Writer) int {
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(stderr, "gen-optionsdb:", err)
		return 2
	}
	var o options
	fl := flag.NewFlagSet("gen-optionsdb", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.StringVar(&o.cache, "cache", filepath.Join(root, ".cache", "gen-optionsdb"), "cache directory (API answers and source archives)")
	fl.StringVar(&o.out, "out", filepath.Join(root, "internal", "optionsdb", "data", "optionsdb.json"), "output file")
	fl.StringVar(&o.overlay, "overlay", filepath.Join(root, "tools", "gen-optionsdb", "overlay.json"), "hand-maintained overlay")
	fl.StringVar(&o.images, "images", filepath.Join(root, "tools", "gen-optionsdb", "images.json"), "image cross-check results")
	fl.BoolVar(&o.offline, "offline", false, "use only the cache; no network")
	fl.BoolVar(&o.verify, "verify-images", false, "cross-check every version against its official Docker image")
	fl.BoolVar(&o.check, "check", false, "do not write; fail if the output file is not up to date")
	fl.StringVar(&o.docker, "docker", "docker", "docker command for -verify-images")
	fl.IntVar(&o.jobs, "jobs", 4, "parallel image checks")
	fl.StringVar(&o.minTag, "min-version", "v0.19.1-1", "oldest mydumper version to embed")
	fl.StringVar(&o.debugDir, "debug-dir", "", "analyse an extracted source tree and print its facts (development aid)")
	fl.StringVar(&o.exportSrc, "export-src", "", "also write the files the extractor reads to DIR/<tag>/, to review overlay evidence")
	fl.StringVar(&o.funcHistory, "func-history", "", "comma-separated C functions: print the version ranges over which each is token-identical, then exit")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintln(stderr, "gen-optionsdb: unexpected arguments:", fl.Args())
		return 2
	}
	if o.debugDir != "" {
		err = debugTree(o.debugDir, stdout)
	} else {
		err = run(o, stdout, stderr)
	}
	if err != nil {
		fmt.Fprintln(stderr, "gen-optionsdb:", err)
		return 1
	}
	return 0
}

func debugTree(dir string, stdout io.Writer) error {
	tree, err := readTreeDir(dir)
	if err != nil {
		return err
	}
	x, err := extract(filepath.Base(dir), tree)
	if err != nil {
		return err
	}
	dumpExtraction(stdout, x)
	return nil
}

// exportTree writes a source tree under dir.
func exportTree(dir string, tree srcTree) error {
	for _, p := range sortedKeys(tree) {
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, tree[p], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// moduleRoot finds the directory holding go.mod, from the working directory
// up.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found; run from the repository")
		}
		dir = parent
	}
}

func run(o options, stdout, stderr io.Writer) error {
	minTag, err := optionsdb.ParseTag(o.minTag)
	if err != nil {
		return err
	}
	up := newUpstream(o.cache, o.offline, os.Getenv("GITHUB_TOKEN"), stderr)
	gh, err := up.github()
	if err != nil {
		return err
	}
	cands, parity, err := selectReleases(gh, minTag, up.imageExists)
	if err != nil {
		return err
	}
	var rels []release
	var tags []string
	for _, c := range cands {
		if c.embedded {
			rels = append(rels, c.release)
			tags = append(tags, c.tag)
		}
	}
	if len(rels) == 0 {
		return errors.New("no version to embed")
	}

	if o.funcHistory != "" {
		names := strings.Split(o.funcHistory, ",")
		prints := make([]map[string][]funcPrint, len(rels))
		for i, r := range rels {
			tree, err := up.tree(r.tag, r.commit)
			if err != nil {
				return err
			}
			if prints[i], err = functionPrints(tree, names); err != nil {
				return fmt.Errorf("%s: %w", r.tag, err)
			}
		}
		printFuncHistory(stdout, names, tags, prints)
		return nil
	}

	xs := make([]*extraction, len(rels))
	for i, r := range rels {
		tree, err := up.tree(r.tag, r.commit)
		if err != nil {
			return err
		}
		if xs[i], err = extract(r.tag, tree); err != nil {
			return err
		}
		if o.exportSrc != "" {
			if err := exportTree(filepath.Join(o.exportSrc, r.tag), tree); err != nil {
				return err
			}
		}
	}
	ov, err := loadOverlay(o.overlay)
	if err != nil {
		return err
	}
	overlayWarnings, err := ov.apply(tags, xs)
	if err != nil {
		return err
	}

	// Parse a first build to get views, then decide image_verified.
	draft, err := buildParsed(rels, xs, nil)
	if err != nil {
		return err
	}
	imgs, err := loadImages(o.images)
	if err != nil {
		return err
	}
	var checked []imageRecord
	if o.verify {
		fmt.Fprintf(stderr, "verifying %d images with %s (%d at a time)\n", len(rels), o.docker, o.jobs)
		checked = verifyImages(context.Background(), execRunner{bin: o.docker}, draft, o.jobs, func(r imageRecord) {
			fmt.Fprintf(stderr, "  %s: verified=%v %s\n", r.Tag, r.Verified, joinProblems(r.Problems))
		})
		imgs = &imagesFile{Comment: imagesComment, Images: checked}
		b, err := encodeJSON(imgs)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(o.images, b); err != nil {
			return err
		}
	}
	verified := map[string]bool{}
	for _, r := range rels {
		rec, ok := imgs.record(r.tag)
		if !ok || !rec.Verified {
			continue
		}
		view, err := draft.View(r.tag, optionsdb.DefaultBuild)
		if err != nil {
			return err
		}
		verified[r.tag] = rec.OptionsSHA256 == officialOptionsHash(view)
	}

	final, err := buildParsed(rels, xs, verified)
	if err != nil {
		return err
	}
	data, err := encodeDB(final)
	if err != nil {
		return err
	}

	printReport(stdout, cands, verified, parity, xs, rels, overlayWarnings, checked)

	if o.check {
		old, err := os.ReadFile(o.out)
		if err != nil {
			return err
		}
		if !bytes.Equal(old, data) {
			return fmt.Errorf("%s is not up to date: run go run ./tools/gen-optionsdb", o.out)
		}
		fmt.Fprintf(stdout, "%s is up to date\n", o.out)
	} else {
		if err := writeFileAtomic(o.out, data); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s (%d versions, %d options)\n", o.out, len(final.Versions), len(final.Options))
	}
	if o.verify {
		for _, r := range checked {
			if r.Mydumper == r.Tag && r.Myloader == r.Tag && !r.Verified {
				return fmt.Errorf("image %s reports the right version but does not match the generated options: fix the generator", r.Tag)
			}
		}
	}
	return nil
}

// buildParsed builds the knowledge base and parses it back, which runs every
// validation of the optionsdb package on the generated data.
func buildParsed(rels []release, xs []*extraction, verified map[string]bool) (*optionsdb.DB, error) {
	db, err := buildDB(rels, xs, verified)
	if err != nil {
		return nil, err
	}
	data, err := encodeDB(db)
	if err != nil {
		return nil, err
	}
	parsed, err := optionsdb.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("generated data does not validate: %w", err)
	}
	return parsed, nil
}
