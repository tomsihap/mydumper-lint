package cli

import (
	"github.com/tomsihap/mydumper-lint/internal/config"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

// resolveTarget maps the configured mydumper version to the knowledge base.
// Until the knowledge base is wired in, only GLib-level rules run and the
// requested version is shown as is.
func resolveTarget(s config.Settings) (model.Target, string, string, error) {
	return nil, s.MydumperVersion, "", nil
}
