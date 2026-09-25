package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseHelp(t *testing.T) {
	help := `Usage:
  mydumper [OPTION…] synthetic tool

First Group
  -r, --rows                  Some text: --not-an-option here
  --ssl                       More text
  --with-arg=VALUE            Takes an argument

Application Options:
  -?, --help                  Help

# rows                        = 0
threads                       = 4
`
	got := parseHelp("mydumper", help)
	want := []string{"mydumper --help -?", "mydumper --rows -r", "mydumper --ssl", "mydumper --with-arg"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// fakeDocker answers docker commands from a table keyed by the arguments.
type fakeDocker struct {
	out  map[string]string
	errs map[string]bool
	ran  []string
}

func (f *fakeDocker) run(_ context.Context, args ...string) (string, string, error) {
	k := strings.Join(args, " ")
	f.ran = append(f.ran, k)
	if f.errs[k] {
		return "", "failure for " + k, errors.New("exit status 1")
	}
	out, ok := f.out[k]
	if !ok {
		return "", "no such command", errors.New("exit status 125")
	}
	return out, "", nil
}

const helpV100 = "Group\n  -r, --rows   x\n  --ssl   x\n"

func newFake(tag, binary, library string, help map[string]string) *fakeDocker {
	img := "mydumper/mydumper:" + tag
	f := &fakeDocker{out: map[string]string{
		"image inspect --format {{json .RepoDigests}} " + img: `["mydumper/mydumper@sha256:abc"]`,
	}, errs: map[string]bool{}}
	for _, tool := range tools {
		f.out["run --rm --entrypoint "+tool+" "+img+" --version"] = tool + " " + binary + ", built against " + library + "\n"
		f.out["run --rm --entrypoint "+tool+" "+img+" --help"] = help[tool]
	}
	return f
}

func TestVerifyImage(t *testing.T) {
	db, err := buildParsed(testReleases(), testExtractions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	goodHelp := map[string]string{"mydumper": helpV100, "myloader": "  -t, --threads   x\n"}
	official := "MySQL 8.4.5 with SSL support"

	t.Run("matching image", func(t *testing.T) {
		rec := verifyImage(context.Background(), newFake("v1.0.0-1", "v1.0.0-1", official, goodHelp), db, "v1.0.0-1")
		if !rec.Verified || len(rec.Problems) > 0 || rec.Digest != "sha256:abc" || rec.Library != "MySQL 8.4.5" || !rec.SSL || rec.OptionsSHA256 == "" {
			t.Errorf("record = %+v", rec)
		}
	})
	t.Run("wrong binary version", func(t *testing.T) {
		rec := verifyImage(context.Background(), newFake("v1.0.1-1", "v1.0.0-1", official, goodHelp), db, "v1.0.1-1")
		if rec.Verified || rec.Mydumper != "v1.0.0-1" || len(rec.Problems) != 1 || !strings.Contains(rec.Problems[0], "contains a v1.0.0-1 binary") {
			t.Errorf("record = %+v", rec)
		}
	})
	t.Run("help mismatch", func(t *testing.T) {
		bad := map[string]string{"mydumper": "  -r, --rows   x\n  --extra   x\n", "myloader": "  --threads   x\n"}
		rec := verifyImage(context.Background(), newFake("v1.0.0-1", "v1.0.0-1", official, bad), db, "v1.0.0-1")
		got := strings.Join(rec.Problems, "\n")
		for _, want := range []string{
			"generated but not in --help: mydumper --ssl",
			"in --help but not generated: mydumper --extra",
			"generated but not in --help: myloader --threads -t",
			"in --help but not generated: myloader --threads",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("problems lack %q:\n%s", want, got)
			}
		}
		if rec.Verified {
			t.Error("verified despite mismatches")
		}
	})
	t.Run("not the official build", func(t *testing.T) {
		rec := verifyImage(context.Background(), newFake("v1.0.0-1", "v1.0.0-1", "MariaDB 11.4.2", goodHelp), db, "v1.0.0-1")
		if rec.Verified || rec.SSL || !strings.Contains(strings.Join(rec.Problems, " "), "not the official build") {
			t.Errorf("record = %+v", rec)
		}
	})
	t.Run("pull failure", func(t *testing.T) {
		f := newFake("v1.0.0-1", "v1.0.0-1", official, goodHelp)
		delete(f.out, "image inspect --format {{json .RepoDigests}} mydumper/mydumper:v1.0.0-1")
		f.errs["pull --quiet mydumper/mydumper:v1.0.0-1"] = true
		rec := verifyImage(context.Background(), f, db, "v1.0.0-1")
		if rec.Verified || len(rec.Problems) != 1 || !strings.Contains(rec.Problems[0], "pull failed") {
			t.Errorf("record = %+v", rec)
		}
	})
	t.Run("all versions in order", func(t *testing.T) {
		var seen []string
		f := newFake("v1.0.0-1", "v1.0.0-1", official, goodHelp)
		recs := verifyImages(context.Background(), f, db, 2, func(r imageRecord) { seen = append(seen, r.Tag) })
		if len(recs) != len(testTags) || recs[0].Tag != "v1.0.0-1" || recs[3].Tag != "v1.1.0-1" || len(seen) != len(testTags) {
			t.Errorf("records = %+v", recs)
		}
	})
}

func TestImagesFile(t *testing.T) {
	f := &imagesFile{Images: []imageRecord{{Tag: "v1.0.0-1", Verified: true}}}
	if r, ok := f.record("v1.0.0-1"); !ok || !r.Verified {
		t.Error("record not found")
	}
	if _, ok := f.record("v9.0.0-1"); ok {
		t.Error("unexpected record")
	}
	empty, err := loadImages("does-not-exist.json")
	if err != nil || len(empty.Images) != 0 {
		t.Errorf("missing file: %v %+v", err, empty)
	}
}
