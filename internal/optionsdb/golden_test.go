package optionsdb

import (
	"slices"
	"testing"
)

// Facts about the embedded knowledge base, checked against the upstream
// source and the official images when the data was generated.

func loadReal(t *testing.T) *DB {
	t.Helper()
	db, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLoadIsCached(t *testing.T) {
	a, b := loadReal(t), loadReal(t)
	if a != b {
		t.Error("Load parsed twice")
	}
}

func TestGoldenVersions(t *testing.T) {
	db := loadReal(t)
	if db.Oldest().Tag != "v0.19.1-1" {
		t.Errorf("oldest = %s", db.Oldest().Tag)
	}
	if _, ok := db.Lookup("v0.20.2-1"); ok {
		t.Error("v0.20.2-1 has neither a release nor an image and must not be embedded")
	}
	for _, tag := range []string{"v0.19.3-3", "v0.20.1-2", "v0.21.3-1", "v1.0.1-3", "v1.0.5-1", "v1.0.8-1"} {
		if _, ok := db.Lookup(tag); !ok {
			t.Errorf("%s not embedded", tag)
		}
	}
	for tag, pre := range map[string]bool{
		"v0.19.3-3": false, "v0.20.1-2": false, "v0.21.3-1": false, "v1.0.5-1": false,
		"v0.21.3-2": true, "v0.21.4-1": true, "v1.0.1-3": true, "v1.0.8-1": true,
	} {
		if v, ok := db.Lookup(tag); !ok || v.Prerelease != pre {
			t.Errorf("%s prerelease = %v, want %v", tag, v.Prerelease, pre)
		}
	}
	for _, v := range db.Versions {
		// the image tagged v1.0.5-1 contains a v1.0.3-1 binary (V3)
		if want := v.Tag != "v1.0.5-1"; v.ImageVerified != want {
			t.Errorf("%s image_verified = %v", v.Tag, v.ImageVerified)
		}
	}
	for spec, want := range map[string]string{"latest": "v1.0.5-1", "0.19": "v0.19.3-3", "0.20": "v0.20.1-2", "0.21": "v0.21.3-1", "1.0": "v1.0.5-1"} {
		r, err := db.Resolve(spec)
		if err != nil || r.Version.Tag != want {
			t.Errorf("Resolve(%s) = %v, %v; want %s", spec, r.Version, err, want)
		}
	}
}

func TestGoldenUnknownOptions(t *testing.T) {
	db := loadReal(t)
	for tag, want := range map[string]bool{"v0.19.3-3": false, "v1.0.0-1": false, "v1.0.1-1": true, "v1.0.8-1": true} {
		if got := mustView(t, db, tag, DefaultBuild).IgnoreUnknownOptions(); got != want {
			t.Errorf("%s ignore_unknown_options = %v, want %v", tag, got, want)
		}
	}
}

func TestGoldenMasquerade(t *testing.T) {
	db := loadReal(t)
	seven := []string{"apply", "constant", "random_format", "random_int", "random_string", "random_uuid", "regex"}
	if got := mustView(t, db, "v0.19.3-3", DefaultBuild).MasqueradeFunctions(); !slices.Equal(got, seven) {
		t.Errorf("v0.19.3-3 masking functions = %q", got)
	}
	newest := ""
	for _, v := range db.Versions {
		if slices.Contains(mustView(t, db, v.Tag, DefaultBuild).MasqueradeFunctions(), "null") {
			newest = v.Tag
		}
	}
	if newest == "" {
		t.Fatal("no version has the null masking function")
	}
	want := append(slices.Clone(seven), "null")
	slices.Sort(want)
	if got := mustView(t, db, newest, DefaultBuild).MasqueradeFunctions(); !slices.Equal(got, want) {
		t.Errorf("%s masking functions = %q, want %q", newest, got, want)
	}
	for _, f := range db.MasqueradeFunctions {
		if !f.Prefix {
			t.Errorf("masking function %s is not selected by prefix", f.Name)
		}
	}
}

func TestGoldenOptions(t *testing.T) {
	db := loadReal(t)
	for _, tag := range []string{"v0.19.1-1", "v0.19.3-3", "v1.0.5-1", "v1.0.8-1"} {
		v := mustView(t, db, tag, DefaultBuild)
		for _, name := range []string{"routines", "threads", "outputdir"} {
			if _, ok := v.Option("mydumper", name); !ok {
				t.Errorf("%s: mydumper --%s missing", tag, name)
			}
		}
		if s, _ := v.Option("mydumper", "threads"); s.Short != "t" || s.Arg != "int" || s.Group != "" {
			t.Errorf("%s: threads = %+v", tag, s)
		}
		if name, ok := v.OptionByShort("mydumper", "t"); !ok || name != "threads" {
			t.Errorf("%s: -t = %q", tag, name)
		}
		if s, _ := v.Option("mydumper", "routines"); s.Arg != "none" {
			t.Errorf("%s: routines = %+v", tag, s)
		}
		if s, ok := v.Option("myloader", "threads"); !ok || s.Short != "t" {
			t.Errorf("%s: myloader threads = %+v", tag, s)
		}
		if _, ok := v.Option("myloader", "outputdir"); ok {
			t.Errorf("%s: outputdir is a mydumper option only", tag)
		}
		if s, ok := v.Option("mydumper", "regex"); !ok || !s.IsRegex || s.Group != "filter" {
			t.Errorf("%s: regex = %+v", tag, s)
		}
		if s, ok := v.Option("mydumper", "snapshot-count"); !ok || s.Group != "daemongroup" {
			t.Errorf("%s: snapshot-count = %+v", tag, s)
		}
		for _, b := range []Build{{ClientMySQL, false}, {ClientMariaDB, false}} {
			if _, ok := mustView(t, db, tag, b).Option("mydumper", "ssl-mode"); ok {
				t.Errorf("%s: ssl-mode present without SSL (%s)", tag, b)
			}
		}
		for _, b := range []Build{{ClientMySQL, true}, {ClientMariaDB, true}} {
			if s, ok := mustView(t, db, tag, b).Option("mydumper", "ssl-mode"); !ok || s.Condition != "WITH_SSL" {
				t.Errorf("%s: ssl-mode with SSL (%s) = %+v, %v", tag, b, s, ok)
			}
		}
	}
	// compress only accepts a closed set from v0.19.3-1 (overlay)
	if s, _ := mustView(t, db, "v0.19.1-1", DefaultBuild).Option("mydumper", "compress"); s.Values != nil {
		t.Errorf("v0.19.1-1 compress values = %q", s.Values)
	}
	if s, _ := mustView(t, db, "v1.0.8-1", DefaultBuild).Option("mydumper", "compress"); !slices.Equal(s.Values, []string{"gzip", "zstd"}) || !s.ValuesIgnoreCase {
		t.Errorf("v1.0.8-1 compress = %+v", s)
	}
	for _, o := range db.Options {
		for _, s := range o.Spans {
			if s.Description != "" {
				t.Fatalf("%s %s carries a description", o.Tool, o.Name)
			}
		}
	}
}

func TestGoldenTableKeys(t *testing.T) {
	db := loadReal(t)
	for _, v := range db.Versions {
		view := mustView(t, db, v.Tag, DefaultBuild)
		for _, k := range []string{"where", "limit", "columns_on_select"} {
			if !view.TableKey(k) {
				t.Errorf("%s: %s is not a table key", v.Tag, k)
			}
		}
		if view.TableKey("wher") || view.TableKey("WHERE") {
			t.Errorf("%s: table keys are exact and case-sensitive", v.Tag)
		}
	}
	latest := mustView(t, db, "v1.0.8-1", DefaultBuild)
	if !latest.TableKey("columns_on_select_replace_`email`") || !latest.TableKey("skip-data-checksums") {
		t.Error("v1.0.8-1 table keys incomplete")
	}
	if mustView(t, db, "v0.19.3-3", DefaultBuild).TableKey("columns_on_select_replace") {
		t.Error("columns_on_select_replace does not exist in v0.19.3-3")
	}
}

func TestGoldenProductsAndLoader(t *testing.T) {
	db := loadReal(t)
	products := mustView(t, db, "v1.0.8-1", DefaultBuild).Products()
	for _, p := range []string{"clickhouse", "dolt", "google", "mariadb", "mysql", "percona", "rds", "tidb", "unknown"} {
		if !slices.Contains(products, p) {
			t.Errorf("v1.0.8-1 products lack %s: %q", p, products)
		}
	}
	// v0.19.1-x loads config files without mydumper's pre-processor; the
	// pre-processor appears in v0.19.3-1 and changes the fingerprint.
	a, _ := db.Lookup("v0.19.1-3")
	b, _ := db.Lookup("v0.19.3-1")
	c, _ := db.Lookup("v0.19.3-3")
	if a.LoaderFingerprint == b.LoaderFingerprint || b.LoaderFingerprint != c.LoaderFingerprint {
		t.Error("loader fingerprints do not reflect the v0.19.3-1 change")
	}
}
