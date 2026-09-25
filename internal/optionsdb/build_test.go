package optionsdb

import "testing"

func TestBuildValidate(t *testing.T) {
	for _, b := range allBuilds {
		if err := b.Validate(); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	for _, c := range []string{"", "MySQL", "percona"} {
		if err := (Build{Client: c}).Validate(); err == nil {
			t.Errorf("client %q: no error", c)
		}
	}
	if DefaultBuild != (Build{Client: "mysql", SSL: true}) {
		t.Errorf("DefaultBuild = %+v", DefaultBuild)
	}
	if s := (Build{Client: "mariadb"}).String(); s != "mariadb/no-ssl" {
		t.Errorf("String = %s", s)
	}
}

func TestConditions(t *testing.T) {
	mysqlSSL, mysqlPlain := Build{ClientMySQL, true}, Build{ClientMySQL, false}
	mariaSSL, mariaPlain := Build{ClientMariaDB, true}, Build{ClientMariaDB, false}
	tests := []struct {
		cond string
		want []bool // mysqlSSL, mysqlPlain, mariaSSL, mariaPlain
	}{
		{"", []bool{true, true, true, true}},
		{"WITH_SSL", []bool{true, false, true, false}},
		{"!WITH_SSL", []bool{false, true, false, true}},
		{"LIBMARIADB", []bool{false, false, true, true}},
		{"WITH_SSL && !LIBMARIADB", []bool{true, false, false, false}},
		{"WITH_SSL && LIBMARIADB", []bool{false, false, true, false}},
		{"!WITH_SSL && LIBMARIADB || WITH_SSL && !LIBMARIADB", []bool{true, false, false, true}},
		{"!(WITH_SSL || LIBMARIADB)", []bool{false, true, false, false}},
		{"HAVE_MY_BOOL", []bool{false, false, false, false}},
		{"!HAVE_MY_BOOL && WITH_SSL", []bool{true, false, true, false}},
		{"!!WITH_SSL", []bool{true, false, true, false}},
	}
	builds := []Build{mysqlSSL, mysqlPlain, mariaSSL, mariaPlain}
	for _, tt := range tests {
		c, err := parseCondition(tt.cond)
		if err != nil {
			t.Errorf("%q: %v", tt.cond, err)
			continue
		}
		for i, b := range builds {
			if got := c.holds(b); got != tt.want[i] {
				t.Errorf("%q with %s = %v, want %v", tt.cond, b, got, tt.want[i])
			}
		}
	}
	for _, bad := range []string{"WITH_TLS", "WITH_SSL &&", "&& WITH_SSL", "(WITH_SSL", "WITH_SSL)", "WITH_SSL & LIBMARIADB", "WITH_SSL LIBMARIADB", "!", "WITH-SSL", "defined(WITH_SSL)"} {
		if _, err := parseCondition(bad); err == nil {
			t.Errorf("parseCondition(%q): no error", bad)
		}
	}
}
