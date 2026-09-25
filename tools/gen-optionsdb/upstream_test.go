package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// makeArchive builds a GitHub-style source archive: a pax global header with
// the commit, and every file under a top-level directory.
func makeArchive(t *testing.T, commit string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: map[string]string{"comment": commit}, Format: tar.FormatPAX,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "repo-x/", Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedKeys(files) {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "repo-x/" + name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadArchive(t *testing.T) {
	data := makeArchive(t, "c1", map[string]string{
		"CMakeLists.txt": "x", "src/a.c": "a", "src/sub/b.h": "b", "README.md": "no", "src/c.txt": "no", "test/t.c": "no",
	})
	tree, err := readArchive(data, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedKeys(tree), " "); got != "CMakeLists.txt src/a.c src/sub/b.h" {
		t.Errorf("files = %s", got)
	}
	if _, err := readArchive(data, "other"); err == nil || !strings.Contains(err.Error(), "archive is commit c1") {
		t.Errorf("commit mismatch: %v", err)
	}
	if _, err := readArchive([]byte("not gzip"), "c1"); err == nil {
		t.Error("garbage: no error")
	}
}

// fakeUpstream serves the three upstream APIs.
type fakeUpstream struct {
	srv      *httptest.Server
	archives map[string][]byte
	hits     atomic.Int32
	auth     atomic.Value
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	f := &fakeUpstream{archives: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		f.auth.Store(r.Header.Get("Authorization"))
		var items []map[string]any
		switch r.URL.Query().Get("page") {
		case "1":
			for i := 0; i < 100; i++ {
				items = append(items, map[string]any{"name": fmt.Sprintf("old-%d", i), "commit": map[string]string{"sha": "x"}})
			}
		case "2":
			items = append(items, map[string]any{"name": "v1.0.0-1", "commit": map[string]string{"sha": "c1"}})
		}
		_ = json.NewEncoder(w).Encode(items)
	})
	mux.HandleFunc("/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v1.0.0-1", "prerelease": true, "draft": false, "published_at": "2026-02-03T04:05:06Z"}})
	})
	mux.HandleFunc("/hub/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/hub/") {
		case "v1.0.0-1":
			_, _ = io.WriteString(w, "{}")
		case "broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	mux.HandleFunc("/codeload/", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		data, ok := f.archives[strings.TrimPrefix(r.URL.Path, "/codeload/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUpstream) client(dir string, offline bool) *upstream {
	u := newUpstream(dir, offline, "tok", io.Discard)
	u.githubAPI = f.srv.URL + "/repo"
	u.codeload = f.srv.URL + "/codeload"
	u.dockerTags = f.srv.URL + "/hub"
	return u
}

func TestUpstreamGitHub(t *testing.T) {
	f := newFakeUpstream(t)
	dir := t.TempDir()
	if _, err := f.client(dir, true).github(); err == nil {
		t.Error("offline without cache: no error")
	}
	gh, err := f.client(dir, false).github()
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.Tags) != 101 || gh.Tags[100] != (ghTag{"v1.0.0-1", "c1"}) {
		t.Errorf("tags: %d, last %+v", len(gh.Tags), gh.Tags[len(gh.Tags)-1])
	}
	if len(gh.Releases) != 1 || !gh.Releases[0].Prerelease || gh.Releases[0].PublishedAt != "2026-02-03T04:05:06Z" {
		t.Errorf("releases = %+v", gh.Releases)
	}
	if got := f.auth.Load(); got != "Bearer tok" {
		t.Errorf("Authorization = %v", got)
	}
	cached, err := f.client(dir, true).github()
	if err != nil || len(cached.Tags) != 101 {
		t.Errorf("offline read: %v", err)
	}
}

func TestUpstreamImages(t *testing.T) {
	f := newFakeUpstream(t)
	dir := t.TempDir()
	u := f.client(dir, false)
	for tag, want := range map[string]bool{"v1.0.0-1": true, "v0.20.2-1": false} {
		got, err := u.imageExists(tag)
		if err != nil || got != want {
			t.Errorf("imageExists(%s) = %v, %v", tag, got, err)
		}
	}
	if _, err := u.imageExists("broken"); err == nil {
		t.Error("server error: no error")
	}
	off := f.client(dir, true)
	if got, err := off.imageExists("v0.20.2-1"); err != nil || got {
		t.Errorf("offline cached answer = %v, %v", got, err)
	}
	if _, err := off.imageExists("v9.9.9-9"); err == nil {
		t.Error("offline unknown tag: no error")
	}
}

func TestUpstreamTree(t *testing.T) {
	f := newFakeUpstream(t)
	commit := strings.Repeat("c", 40)
	f.archives[commit] = makeArchive(t, commit, map[string]string{"CMakeLists.txt": "x", "src/a.c": "int a;"})
	dir := t.TempDir()
	if _, err := f.client(dir, true).tree("v1.0.0-1", commit); err == nil {
		t.Error("offline without cache: no error")
	}
	for i := 0; i < 2; i++ {
		tree, err := f.client(dir, false).tree("v1.0.0-1", commit)
		if err != nil || string(tree["src/a.c"]) != "int a;" {
			t.Fatalf("tree: %v %v", tree, err)
		}
	}
	if f.hits.Load() != 1 {
		t.Errorf("downloaded %d times, want once", f.hits.Load())
	}
	if _, err := f.client(dir, true).tree("v1.0.0-1", commit); err != nil {
		t.Errorf("offline cached tree: %v", err)
	}
	wrong := strings.Repeat("d", 40)
	f.archives[wrong] = makeArchive(t, commit, map[string]string{"src/a.c": "x"})
	if _, err := f.client(dir, false).tree("v1.0.1-1", wrong); err == nil {
		t.Error("archive of another commit: no error")
	}
}
