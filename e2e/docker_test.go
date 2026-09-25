//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// Test-only constants shared with compose.yaml and fixtures/seed.sql.
const (
	composeFile       = "compose.yaml"
	composeService    = "mysql"
	network           = "mydumper-lint-e2e" // networks.default.name in compose.yaml
	mysqlHost         = "mysql"             // the service name on that network
	mysqlRootPassword = "e2e-root-password"
	e2eUser           = "e2e"
	e2ePassword       = "e2e-password"
	containerLabel    = "mydumper-lint-e2e" // on every tool container, for `make e2e-down`
	imageRepository   = "mydumper/mydumper"
	sourceDumpDir     = "source-dump" // myloader scenarios restore this dump
	imagesRecord      = "../tools/gen-optionsdb/images.json"
)

// suite is the state shared by every scenario run.
type suite struct {
	db       *optionsdb.DB
	versions []string // selected versions (the matrix)
	selector string   // how they were selected, for the summary
	fallback bool     // scenarios no selected version applies to run on their own version
	mysqlCID string   // container ID of the MySQL service
	lintBin  string   // mydumper-lint built from this checkout
	user     string   // --user for tool containers ("uid:gid"), empty on Windows
	outDir   string   // e2e/.out
	keep     bool     // keep every work directory (E2E_KEEP=1)
	lintOnly bool     // E2E_LINT_ONLY=1: check the lint expectations only, without Docker
	timeout  time.Duration
	digests  map[string]string // tag → image digest verified by the generator

	mu     sync.Mutex
	guards map[string]*guard
}

// guard is the image check of one version, done once.
type guard struct {
	once sync.Once
	ref  string // image reference to run
	desc string // what the binaries report
	skip string // non-empty: the reason the version cannot run
}

var (
	suiteOnce sync.Once
	theSuite  *suite
	errSuite  error
)

// getSuite starts the environment on first use: knowledge base, version
// matrix, MySQL service, linter binary and image guards.
func getSuite() (*suite, error) {
	suiteOnce.Do(func() { theSuite, errSuite = newSuite() })
	return theSuite, errSuite
}

func newSuite() (*suite, error) {
	s := &suite{guards: map[string]*guard{}, timeout: 5 * time.Minute}
	var err error
	if s.db, err = optionsdb.Load(); err != nil {
		return nil, err
	}
	if s.versions, s.selector, s.fallback, err = selectVersions(s.db, os.Getenv("E2E_VERSIONS")); err != nil {
		return nil, err
	}
	s.keep = os.Getenv("E2E_KEEP") == "1"
	if v := os.Getenv("E2E_TIMEOUT"); v != "" {
		if s.timeout, err = time.ParseDuration(v); err != nil {
			return nil, fmt.Errorf("E2E_TIMEOUT: %w", err)
		}
	}
	if runtime.GOOS != "windows" {
		s.user = fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	}
	if s.outDir, err = filepath.Abs(".out"); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.outDir, 0o755); err != nil {
		return nil, err
	}
	s.digests = readDigests(imagesRecord)

	ctx := context.Background()
	if s.lintOnly = os.Getenv("E2E_LINT_ONLY") == "1"; s.lintOnly {
		logf("E2E_LINT_ONLY=1: checking the lint expectations only, without Docker")
		if err := s.buildLinter(ctx); err != nil {
			return nil, err
		}
		return s, nil
	}
	if _, err := s.docker(ctx, time.Minute, "version", "--format", "{{.Server.Version}}"); err != nil {
		return nil, fmt.Errorf("docker is not usable: %w", err)
	}
	logf("starting the MySQL service (docker compose -f e2e/%s up --wait)", composeFile)
	if _, err := s.docker(ctx, 10*time.Minute, "compose", "-f", composeFile, "up", "--detach", "--wait"); err != nil {
		return nil, fmt.Errorf("docker compose up: %w", err)
	}
	out, err := s.docker(ctx, time.Minute, "compose", "-f", composeFile, "ps", "--quiet", composeService)
	if err != nil {
		return nil, err
	}
	if s.mysqlCID = strings.TrimSpace(out.Stdout); s.mysqlCID == "" {
		return nil, errors.New("docker compose ps: no container for the mysql service")
	}
	if err := s.buildLinter(ctx); err != nil {
		return nil, err
	}
	for _, tag := range s.versions {
		s.image(tag) // guard every selected version up front: loud messages first
	}
	return s, nil
}

// selectVersions returns the versions E2E_VERSIONS selects: the default
// matrix (the latest stable image-verified release of each branch), every
// image-verified version ("all"), or a comma-separated list of tags. fallback
// reports whether scenarios outside the selection run on a version of their
// own (default matrix only).
func selectVersions(db *optionsdb.DB, spec string) (tags []string, selector string, fallback bool, err error) {
	spec = strings.TrimSpace(spec)
	switch spec {
	case "", "default":
		latest := map[[2]int]optionsdb.Version{}
		var order [][2]int
		for _, v := range db.Versions {
			if !v.ImageVerified || v.Prerelease {
				continue
			}
			t := optionsdb.MustParseTag(v.Tag)
			branch := [2]int{t.Major, t.Minor}
			if _, ok := latest[branch]; !ok {
				order = append(order, branch)
			}
			latest[branch] = v // db.Versions is sorted ascending
		}
		for _, b := range order {
			tags = append(tags, latest[b].Tag)
		}
		return tags, "default (latest stable image-verified release of each branch)", true, nil
	case "all":
		for _, v := range db.Versions {
			if v.ImageVerified {
				tags = append(tags, v.Tag)
			}
		}
		return tags, "all (every image-verified version)", false, nil
	}
	seen := map[string]bool{}
	for part := range strings.SplitSeq(spec, ",") {
		t, err := optionsdb.ParseTag(strings.TrimSpace(part))
		if err != nil {
			return nil, "", false, fmt.Errorf("E2E_VERSIONS: %w", err)
		}
		v, ok := db.Lookup(t.String())
		if !ok {
			return nil, "", false, fmt.Errorf("E2E_VERSIONS: %s is not in the knowledge base", t)
		}
		if !seen[v.Tag] {
			seen[v.Tag] = true
			tags = append(tags, v.Tag)
		}
	}
	return tags, "E2E_VERSIONS=" + spec, false, nil
}

// readDigests reads the image digests the generator recorded when it
// cross-checked the images (tools/gen-optionsdb -verify-images): the suite
// runs exactly the images the knowledge base was verified against.
func readDigests(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		logf("WARNING: %v: images run by tag, not by digest", err)
		return out
	}
	var rec struct {
		Images []struct {
			Tag    string `json:"tag"`
			Digest string `json:"digest"`
		} `json:"images"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		logf("WARNING: %s: %v: images run by tag, not by digest", path, err)
		return out
	}
	for _, im := range rec.Images {
		if strings.HasPrefix(im.Digest, "sha256:") {
			out[im.Tag] = im.Digest
		}
	}
	return out
}

// cmdResult is the outcome of a docker command.
type cmdResult struct {
	Stdout, Stderr string
	ExitCode       int
	Duration       time.Duration
}

// docker runs the docker CLI. It returns an error when docker cannot run,
// times out, or exits with a non-zero code (the result is still returned).
func (s *suite) docker(ctx context.Context, timeout time.Duration, args ...string) (cmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	r := cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(start)}
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return r, fmt.Errorf("docker %s: timed out after %s", firstWords(args), timeout)
	case errors.As(err, &exit):
		r.ExitCode = exit.ExitCode()
		return r, fmt.Errorf("docker %s: exit %d: %s", firstWords(args), r.ExitCode, firstLine(r.Stderr))
	case err != nil:
		return r, fmt.Errorf("docker %s: %w", firstWords(args), err)
	}
	return r, nil
}

// image runs the image guard of a version once: the official image, pinned
// by the digest the generator verified, must contain mydumper and myloader
// binaries that report exactly this version (design §3.8 V3: the
// v1.0.5-1 image ships a v1.0.3-1 binary). A version that fails is skipped
// with a loud message, never run.
func (s *suite) image(tag string) *guard {
	s.mu.Lock()
	g, ok := s.guards[tag]
	if !ok {
		g = &guard{}
		s.guards[tag] = g
	}
	s.mu.Unlock()
	g.once.Do(func() {
		g.ref = imageRepository + ":" + tag
		if d, ok := s.digests[tag]; ok {
			g.ref += "@" + d
		}
		if s.lintOnly {
			g.desc = "not checked (E2E_LINT_ONLY=1)"
			return
		}
		ctx := context.Background()
		if _, err := s.docker(ctx, time.Minute, "image", "inspect", "--format", "{{.Id}}", g.ref); err != nil {
			logf("pulling %s", g.ref)
			if _, err := s.docker(ctx, 20*time.Minute, "pull", "--quiet", g.ref); err != nil {
				g.skip = fmt.Sprintf("IMAGE GUARD: %s cannot be pulled: %v", g.ref, err)
			}
		}
		if v, ok := s.db.Lookup(tag); ok && !v.ImageVerified && g.skip == "" {
			logf("WARNING: the knowledge base does not mark the image of %s as verified", tag)
		}
		var got []string
		for _, tool := range knownTools {
			if g.skip != "" {
				break
			}
			out, err := s.docker(ctx, 3*time.Minute, "run", "--rm", "--label", containerLabel, "--entrypoint", tool, g.ref, "--version")
			line := firstLine(out.Stdout)
			m := versionRE.FindStringSubmatch(line)
			switch {
			case err != nil:
				g.skip = fmt.Sprintf("IMAGE GUARD: %s --version failed in %s: %v", tool, g.ref, err)
			case len(m) != 3 || m[1] != tool:
				g.skip = fmt.Sprintf("IMAGE GUARD: %s --version in %s printed %q", tool, g.ref, line)
			case "v"+m[2] != tag:
				g.skip = fmt.Sprintf("IMAGE GUARD: image %s contains a %s v%s binary, not %s", g.ref, tool, m[2], tag)
			default:
				got = append(got, line)
			}
		}
		if g.skip != "" {
			banner(g.skip + "; version " + tag + " is SKIPPED")
			return
		}
		g.desc = strings.Join(got, "; ")
		// The image's own environment: the locale scenarios rely on it setting
		// no LANG or LC_* variable (the harness only adds a scenario's env: lines).
		if out, err := s.docker(ctx, time.Minute, "image", "inspect", "--format", "{{json .Config.Env}}", g.ref); err == nil {
			g.desc += "; image env " + strings.TrimSpace(out.Stdout)
		}
	})
	return g
}

var versionRE = regexp.MustCompile(`^(mydumper|myloader) v(\d+\.\d+\.\d+-\d+), built against `)

// buildLinter builds mydumper-lint from this checkout once. Scenarios then
// run the equivalent of `go run ./cmd/mydumper-lint check …`.
func (s *suite) buildLinter(ctx context.Context) error {
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("module root not found: %w", err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return err
	}
	s.lintBin = filepath.Join(s.outDir, "bin", "mydumper-lint")
	if runtime.GOOS == "windows" {
		s.lintBin += ".exe"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", s.lintBin, "./cmd/mydumper-lint")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build ./cmd/mydumper-lint: %w\n%s", err, out)
	}
	return nil
}

// runTool runs mydumper or myloader in a fresh container on the compose
// network, with dir mounted as /work (also the working directory).
func (s *suite) runTool(ctx context.Context, ref, tool, dir string, env, args []string) (cmdResult, error) {
	name := "mydumper-lint-e2e-" + randomHex(6)
	full := []string{
		"run", "--rm", "--name", name, "--label", containerLabel,
		"--network", network, "--volume", dir + ":/work", "--workdir", "/work",
	}
	if s.user != "" {
		full = append(full, "--user", s.user)
	}
	for _, e := range env {
		full = append(full, "--env", e)
	}
	full = append(full, "--entrypoint", tool, ref)
	full = append(full, args...)
	r, err := s.docker(ctx, s.timeout, full...)
	// Exit code 125 is docker's own failure (no image, no network…); 0 with an
	// error is a timeout. Either way the tool did not run to completion.
	if err != nil && (r.ExitCode == 0 || r.ExitCode == 125) {
		_, _ = s.docker(context.Background(), time.Minute, "rm", "--force", name)
		return r, err
	}
	return r, nil // a non-zero exit code of the tool is an observation, not an error
}

// sql runs a statement as root on the MySQL service and returns its output.
func (s *suite) sql(ctx context.Context, stmt string) (cmdResult, error) {
	return s.docker(ctx, time.Minute, "exec", "--env", "MYSQL_PWD="+mysqlRootPassword, s.mysqlCID,
		"mysql", "--user=root", "--batch", "--skip-column-names", "--execute", stmt)
}

// countRows returns the number of rows of db.table, or "missing".
func (s *suite) countRows(db, table string) (string, error) {
	r, err := s.sql(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", quoteIdent(db), quoteIdent(table)))
	if err != nil {
		if strings.Contains(r.Stderr, "doesn't exist") {
			return "missing", nil
		}
		return "", err
	}
	n := strings.TrimSpace(r.Stdout)
	if _, err := strconv.Atoi(n); err != nil {
		return "", fmt.Errorf("unexpected row count %q", n)
	}
	return n, nil
}

// dropDatabase removes a restore database.
func (s *suite) dropDatabase(db string) error {
	_, err := s.sql(context.Background(), "DROP DATABASE IF EXISTS "+quoteIdent(db))
	return err
}

func quoteIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func firstWords(args []string) string {
	if len(args) > 2 {
		args = args[:2]
	}
	return strings.Join(args, " ")
}

// logf prints progress to stderr, which go test shows even without -v.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "e2e: "+format+"\n", args...)
}

// banner prints a message nobody can miss.
func banner(msg string) {
	line := strings.Repeat("!", 78)
	fmt.Fprintf(os.Stderr, "\n%s\n!! %s\n%s\n\n", line, msg, line)
}
