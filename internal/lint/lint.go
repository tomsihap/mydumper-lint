// Package lint ties the pipeline together (design §4.2): source, pre-processor,
// GKeyFile emulation, model, rules, and the fixer with its self-check.
package lint

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/fix"
	"github.com/tomsihap/mydumper-lint/internal/goption"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/rules"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// Config configures a Linter.
type Config struct {
	Selection rules.Selection
	Target    model.Target // nil: no mydumper version (GLib-level rules only)
	Version   string       // resolved mydumper version tag, for messages
	Languages []string     // runtime language list of mydumper (K14); nil means ["C"]
	// NoPreprocessor is set for mydumper versions that load the file with GLib
	// directly (v0.19.1-x): no "= 1" rewrite, no bracket state leak.
	NoPreprocessor bool
	// Charset is the character set of mydumper's locale (model.Options).
	Charset goption.Charset
}

// Linter checks and fixes files with one configuration. It is safe for
// concurrent use.
type Linter struct {
	cfg     Config
	enabled []rules.Enabled
}

// New resolves the rule selection.
func New(cfg Config) (*Linter, error) {
	enabled, err := cfg.Selection.Resolve()
	if err != nil {
		return nil, err
	}
	return &Linter{cfg: cfg, enabled: enabled}, nil
}

// Enabled returns the enabled rules.
func (l *Linter) Enabled() []rules.Enabled { return l.enabled }

// Result is everything known about one file.
type Result struct {
	File        *source.File
	Pre         *preprocess.Result
	KF          *keyfile.Result
	Model       *model.Model
	Diagnostics []diag.Diagnostic
}

func (l *Linter) modelOptions() model.Options {
	return model.Options{Languages: l.cfg.Languages, Target: l.cfg.Target, Charset: l.cfg.Charset}
}

// preprocess runs mydumper's pre-processor, or not, as the target version does.
func (l *Linter) preprocess(f *source.File) *preprocess.Result {
	if l.cfg.NoPreprocessor {
		return preprocess.Passthrough(f)
	}
	return preprocess.Run(f)
}

// Check lints src.
func (l *Linter) Check(path string, src []byte) *Result {
	f := source.New(path, src)
	pre := l.preprocess(f)
	kf := keyfile.Parse(f, pre)
	m := model.Build(kf, l.modelOptions())
	p := rules.NewPass(f, pre, kf, m)
	p.Target, p.Version, p.Languages = l.cfg.Target, l.cfg.Version, l.cfg.Languages
	p.Preprocessor = !l.cfg.NoPreprocessor
	ds := p.Run(l.enabled)
	return &Result{File: f, Pre: pre, KF: kf, Model: m, Diagnostics: ds}
}

// Measure returns the projection of the recovered model of src and the
// health of src itself, for the fixer's self-check (design §7.3).
func (l *Linter) Measure(src []byte) (string, model.Health) {
	f := source.New("", src)
	health := model.Build(keyfile.Parse(f, l.preprocess(f)), l.modelOptions()).Health
	rf := source.New("", keyfile.RecoverWith(src, l.cfg.NoPreprocessor))
	recovered := model.Build(keyfile.Parse(rf, l.preprocess(rf)), l.modelOptions())
	return recovered.Projection(), health
}

// health is the health of src (design §7.3).
func (l *Linter) health(src []byte) model.Health {
	f := source.New("", src)
	return model.Build(keyfile.Parse(f, l.preprocess(f)), l.modelOptions()).Health
}

// Fix applies fixes until nothing changes, then runs the self-check. On a
// self-check failure (a *fix.SelfCheckError) nothing must be written.
func (l *Linter) Fix(path string, src []byte, opt fix.Options) (fix.Result, error) {
	opt.Health = l.health
	res, err := fix.Fixpoint(src, func(b []byte) []diag.Diagnostic {
		return l.Check(path, b).Diagnostics
	}, opt)
	if err != nil {
		return res, err
	}
	if err := fix.SelfCheck(src, res.Output, res.UnsafeApplied, l.Measure); err != nil {
		return res, err
	}
	return res, nil
}
