package cli

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tomsihap/mydumper-lint/internal/config"
	"github.com/tomsihap/mydumper-lint/internal/lint"
)

// targetCache builds one linter per distinct effective settings.
type targetCache struct {
	mu      sync.Mutex
	linters map[string]cachedLinter
}

type cachedLinter struct {
	l                *lint.Linter
	version, warning string
	err              error
}

func newTargetCache() *targetCache {
	return &targetCache{linters: map[string]cachedLinter{}}
}

// linter returns the linter for settings s, the resolved mydumper version and
// a notice about the version resolution (empty when there is nothing to say).
func (t *targetCache) linter(s config.Settings) (*lint.Linter, string, string, error) {
	key := settingsKey(s)
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.linters[key]; ok {
		return c.l, c.version, c.warning, c.err
	}
	target, version, warning, err := resolveTarget(s)
	var c cachedLinter
	if err == nil {
		c.l, err = lint.New(lint.Config{Selection: s.Selection, Target: target, Version: version})
	}
	c.version, c.warning, c.err = version, warning, err
	t.linters[key] = c
	return c.l, c.version, c.warning, c.err
}

func settingsKey(s config.Settings) string {
	var sev []string
	for k, v := range s.Selection.Severity {
		sev = append(sev, k+"="+v.String())
	}
	sort.Strings(sev)
	ssl := s.Build.SSL != nil && *s.Build.SSL
	return fmt.Sprintf("%s|%s|%v|%s|%s|%s|%s|%v",
		s.MydumperVersion, s.Build.Client, ssl,
		strings.Join(s.Selection.Select, ","), strings.Join(s.Selection.ExtendSelect, ","),
		strings.Join(s.Selection.Ignore, ","), strings.Join(sev, ","), s.Selection.Preview)
}
