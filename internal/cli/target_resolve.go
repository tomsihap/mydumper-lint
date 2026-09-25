package cli

import (
	"github.com/tomsihap/mydumper-lint/internal/config"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/target"
)

// resolveTarget maps the configured mydumper version and build to the
// knowledge base.
func resolveTarget(s config.Settings) (target.Target, error) {
	b := optionsdb.Build{Client: s.Build.Client, SSL: s.Build.SSL == nil || *s.Build.SSL}
	return target.Resolve(s.MydumperVersion, b)
}
