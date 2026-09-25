package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// upstream reads facts about mydumper releases: git tags and GitHub release
// objects (REST API), official image tags (Docker Hub API) and source
// archives. Everything is cached under dir; with offline set, only the cache
// is used.
type upstream struct {
	dir     string
	offline bool
	token   string // GitHub token; optional
	client  *http.Client
	log     io.Writer

	// endpoints, overridable in tests
	githubAPI  string // https://api.github.com/repos/mydumper/mydumper
	codeload   string // https://codeload.github.com/mydumper/mydumper/tar.gz
	dockerTags string // https://hub.docker.com/v2/repositories/mydumper/mydumper/tags
}

func newUpstream(dir string, offline bool, token string, log io.Writer) *upstream {
	return &upstream{
		dir: dir, offline: offline, token: token, log: log,
		client:     &http.Client{Timeout: 5 * time.Minute},
		githubAPI:  "https://api.github.com/repos/mydumper/mydumper",
		codeload:   "https://codeload.github.com/mydumper/mydumper/tar.gz",
		dockerTags: "https://hub.docker.com/v2/repositories/mydumper/mydumper/tags",
	}
}

type ghTag struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

type ghRelease struct {
	Tag         string `json:"tag"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
	PublishedAt string `json:"published_at"`
}

// githubCache is the normalized content of the GitHub API answers.
type githubCache struct {
	Tags     []ghTag     `json:"tags"`
	Releases []ghRelease `json:"releases"`
}

// github returns the tags and releases of the repository.
func (u *upstream) github() (*githubCache, error) {
	path := filepath.Join(u.dir, "github.json")
	if u.offline {
		var c githubCache
		if err := readJSON(path, &c); err != nil {
			return nil, fmt.Errorf("offline: %w (run once without -offline to fill the cache)", err)
		}
		return &c, nil
	}
	var c githubCache
	var rawTags []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := u.paginate(u.githubAPI+"/tags", &rawTags); err != nil {
		return nil, err
	}
	for _, t := range rawTags {
		c.Tags = append(c.Tags, ghTag{Name: t.Name, Commit: t.Commit.SHA})
	}
	var rawReleases []struct {
		TagName     string `json:"tag_name"`
		Prerelease  bool   `json:"prerelease"`
		Draft       bool   `json:"draft"`
		PublishedAt string `json:"published_at"`
	}
	if err := u.paginate(u.githubAPI+"/releases", &rawReleases); err != nil {
		return nil, err
	}
	for _, r := range rawReleases {
		c.Releases = append(c.Releases, ghRelease{Tag: r.TagName, Prerelease: r.Prerelease, Draft: r.Draft, PublishedAt: r.PublishedAt})
	}
	if err := writeJSON(path, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// paginate fetches every page of a GitHub list endpoint into out (a pointer
// to a slice).
func (u *upstream) paginate(url string, out any) error {
	var all []json.RawMessage
	for page := 1; ; page++ {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s?per_page=100&page=%d", url, page), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "mydumper-lint-gen-optionsdb")
		if u.token != "" {
			req.Header.Set("Authorization", "Bearer "+u.token)
		}
		body, err := u.do(req)
		if err != nil {
			return err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		all = append(all, items...)
		if len(items) < 100 {
			break
		}
	}
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (u *upstream) do(req *http.Request) ([]byte, error) {
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s: %s", req.URL, resp.Status, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	return body, nil
}

// dockerCache records which official image tags exist.
type dockerCache map[string]bool

// imageExists asks Docker Hub whether mydumper/mydumper:<tag> exists.
func (u *upstream) imageExists(tag string) (bool, error) {
	path := filepath.Join(u.dir, "dockerhub.json")
	cache := dockerCache{}
	if err := readJSON(path, &cache); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if u.offline {
		v, ok := cache[tag]
		if !ok {
			return false, fmt.Errorf("offline: no cached Docker Hub answer for %s (run once without -offline)", tag)
		}
		return v, nil
	}
	req, err := http.NewRequest(http.MethodGet, u.dockerTags+"/"+tag, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "mydumper-lint-gen-optionsdb")
	resp, err := u.client.Do(req)
	if err != nil {
		return false, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	var exists bool
	switch resp.StatusCode {
	case http.StatusOK:
		exists = true
	case http.StatusNotFound:
		exists = false
	default:
		return false, fmt.Errorf("Docker Hub %s: %s", tag, resp.Status)
	}
	cache[tag] = exists
	return exists, writeJSON(path, cache)
}

// tree returns the source files of a commit, downloading its archive into
// the cache the first time.
func (u *upstream) tree(tag, commit string) (srcTree, error) {
	path := filepath.Join(u.dir, "src", fmt.Sprintf("mydumper-%s-%s.tar.gz", tag, commit))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if u.offline {
			return nil, fmt.Errorf("offline: %s is not in the cache", tag)
		}
		fmt.Fprintf(u.log, "downloading %s (%s)\n", tag, commit[:12])
		req, err := http.NewRequest(http.MethodGet, u.codeload+"/"+commit, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "mydumper-lint-gen-optionsdb")
		data, err = u.do(req)
		if err != nil {
			return nil, err
		}
		if _, err := readArchive(data, commit); err != nil {
			return nil, fmt.Errorf("%s: %w", tag, err)
		}
		if err := writeFileAtomic(path, data); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	tree, err := readArchive(data, commit)
	if err != nil {
		return nil, fmt.Errorf("%s (%s): %w", tag, path, err)
	}
	return tree, nil
}

// readArchive reads the files the extractor needs from a GitHub source
// archive, stripping the top-level directory, and checks that the archive is
// the expected commit when it says so.
func readArchive(data []byte, commit string) (srcTree, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	tree := srcTree{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			if c, ok := h.PAXRecords["comment"]; ok && c != commit {
				return nil, fmt.Errorf("archive is commit %s, want %s", c, commit)
			}
			continue
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		_, rel, ok := strings.Cut(h.Name, "/")
		if !ok || !keepPath(rel) {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		tree[rel] = b
	}
	if len(tree) == 0 {
		return nil, errors.New("archive has no source file")
	}
	return tree, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'))
}

// writeFileAtomic writes through a temporary file in the same directory.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
