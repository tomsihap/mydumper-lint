//go:build integration

package lint_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/lint"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/target"
)

// upstreamExamples are the example configuration files at the root of the
// mydumper repository; a tag may lack one.
var upstreamExamples = []string{"mydumper.cnf", "myloader.cnf"}

// TestUpstreamExamples lints the example configuration files of every
// embedded mydumper tag against their own version: they must give no error
// (design §11.7). The files are GPL-3.0, so they are downloaded at test time,
// cached in .cache/upstream-cnf/ (git-ignored), and never vendored.
func TestUpstreamExamples(t *testing.T) {
	db, err := optionsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join("..", "..", ".cache", "upstream-cnf")
	client := &http.Client{Timeout: time.Minute}
	linted := 0
	for _, v := range db.Versions {
		tg, err := target.Resolve(v.Tag, optionsdb.DefaultBuild)
		if err != nil {
			t.Fatal(err)
		}
		var cfg lint.Config
		tg.Configure(&cfg)
		l, err := lint.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range upstreamExamples {
			src, err := fetchUpstream(client, cache, v.Commit, name)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Fatalf("%s %s: %v", v.Tag, name, err)
			}
			linted++
			for _, d := range l.Check(name, src).Diagnostics {
				if d.Severity == diag.Error {
					t.Errorf("%s %s: %s %s", v.Tag, name, d.RuleID, d.Message)
				}
			}
		}
	}
	if linted < len(db.Versions) {
		t.Errorf("linted %d example files for %d versions: the layout of the repository changed", linted, len(db.Versions))
	}
}

// fetchUpstream returns a file of the mydumper repository at a commit, from
// the cache or GitHub. A missing file is os.ErrNotExist, and is cached too.
func fetchUpstream(client *http.Client, cache, commit, name string) ([]byte, error) {
	path := filepath.Join(cache, commit, name)
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}
	if _, err := os.Stat(path + ".missing"); err == nil {
		return nil, os.ErrNotExist
	}
	resp, err := client.Get("https://raw.githubusercontent.com/mydumper/mydumper/" + commit + "/" + name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errors.Join(os.ErrNotExist, os.WriteFile(path+".missing", nil, 0o644))
	default:
		return nil, fmt.Errorf("GET %s: %s", name, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return b, os.WriteFile(path, b, 0o644)
}
