package model

import (
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

func build(s string) *Model {
	f := source.New("t.cnf", []byte(s))
	return Build(keyfile.Parse(f, preprocess.Run(f)), Options{Languages: []string{"C"}})
}

func TestClassifyGroup(t *testing.T) {
	tests := []struct {
		name string
		kind GroupKind
		tool string
	}{
		{"mydumper", GroupToolOptions, "mydumper"},
		{"myloader", GroupToolOptions, "myloader"},
		{"MyDumper", GroupUnknown, ""},
		{" mydumper ", GroupUnknown, ""},
		{"mydumper_mysql", GroupProductOptions, "mydumper"},
		{"myloader_mariadb_10_6", GroupProductOptions, "myloader"},
		{"mydumper_mysql_8_0_36", GroupProductOptions, "mydumper"},
		{"mydumper_mysql_8_0_36_1", GroupUnknown, ""},
		{"mydumper_mysql_x", GroupUnknown, ""},
		{"mydumper_oracle", GroupUnknown, ""},
		{"mydumper_session_variables", GroupSessionVariables, "mydumper"},
		{"myloader_session_variables_mariadb", GroupSessionVariables, "myloader"},
		{"myloader_global_variables_mysql_8", GroupGlobalVariables, "myloader"},
		{"mydumper_variables", GroupUnknown, ""}, // renamed in v0.14.0-1
		{"client", GroupClient, ""},
		{"`db`.`t`", GroupTable, ""},
		{"`db`.``", GroupTable, ""},
		{"``.`t`", GroupTable, ""},
		{"db.t", GroupUnknown, ""},
		{"`db.t`", GroupUnknown, ""},
	}
	for _, tt := range tests {
		kind, tool := ClassifyGroup(tt.name, nil)
		if kind != tt.kind || tool != tt.tool {
			t.Errorf("ClassifyGroup(%q) = %v, %q; want %v, %q", tt.name, kind, tool, tt.kind, tt.tool)
		}
	}
}

func TestBuildReasons(t *testing.T) {
	m := build("[mydumper]\nthreads=4\nhost=db\nthreads=8\nk[fr]=x\n[other]\na=1\n[client]\nuser=u\n")
	if m.Health != OK {
		t.Fatalf("health = %v", m.Health)
	}
	var got []string
	for _, g := range m.Groups {
		for _, e := range g.Entries {
			got = append(got, g.Name+"."+e.Key+"="+e.Value+":"+e.Reason.String())
		}
	}
	want := []string{
		"mydumper.threads=8:shadowed-by-duplicate", // first occurrence: its value is lost
		"mydumper.host=db:connection-key",
		"mydumper.threads=8:effective",
		"mydumper.k[fr]=x:localized",
		"other.a=1:unknown-group",
		"client.user=u:effective",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("entries:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRejectedFileAppliesNothing(t *testing.T) {
	m := build("[mydumper]\nthreads=4\n  \n")
	if m.Health != Rejected || m.Projection() != "" {
		t.Errorf("health=%v projection=%q", m.Health, m.Projection())
	}
	for _, g := range m.Groups {
		for _, e := range g.Entries {
			if e.Effective || e.Reason != ReasonFileRejected {
				t.Errorf("%s.%s: effective=%v reason=%v", g.Name, e.Key, e.Effective, e.Reason)
			}
		}
	}
}

func TestProjectionIgnoresFormatting(t *testing.T) {
	a := build("[mydumper]\nthreads=4\nroutines\n\n# comment\n[`db`.`t`]\nwhere=x > 1\n")
	b := build("  [mydumper]\nthreads = 4\n\nroutines=1\n[`db`.`t`]\nwhere =x > 1\n[unknown]\nz=1\n")
	if a.Projection() != b.Projection() {
		t.Errorf("projections differ:\n%s\n---\n%s", a.Projection(), b.Projection())
	}
	c := build("[mydumper]\nthreads=5\nroutines\n[`db`.`t`]\nwhere=x > 1\n")
	if a.Projection() == c.Projection() {
		t.Error("a value change must change the projection")
	}
}

func TestRecoveredModel(t *testing.T) {
	// The recovered model of a rejected file is what the author meant.
	b := []byte("[mydumper]\nthreads=4\n  \n[myloader]\nthreads=2\n")
	f := source.New("t.cnf", keyfile.Recover(b))
	m := Build(keyfile.Parse(f, preprocess.Run(f)), Options{Languages: []string{"C"}})
	want := "[mydumper]\n\"threads\"=\"4\"\n[myloader]\n\"threads\"=\"2\"\n"
	if m.Health != OK || m.Projection() != want {
		t.Errorf("health=%v projection=%q, want %q", m.Health, m.Projection(), want)
	}
}

func TestStrings(t *testing.T) {
	for h := Rejected; h <= OK; h++ {
		if strings.HasPrefix(h.String(), "Health(") {
			t.Errorf("health %d has no name", h)
		}
	}
	for r := ReasonEffective; r <= ReasonAfterEndOfOptions; r++ {
		if strings.HasPrefix(r.String(), "Reason(") {
			t.Errorf("reason %d has no name", r)
		}
	}
	for k := GroupUnknown; k <= GroupTable; k++ {
		if strings.HasPrefix(k.String(), "GroupKind(") {
			t.Errorf("group kind %d has no name", k)
		}
	}
}
