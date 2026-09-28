package model

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/goption"
)

func TestStringBounds(t *testing.T) {
	if !strings.HasPrefix((OK+1).String(), "Health(") || !strings.HasPrefix((GroupTable+1).String(), "GroupKind(") ||
		!strings.HasPrefix((ReasonDefaultsFileRejected+1).String(), "Reason(") {
		t.Error("the first value past each list must be out of range")
	}
	for r := ReasonEffective; r <= ReasonDefaultsFileRejected; r++ {
		if strings.HasPrefix(r.String(), "Reason(") {
			t.Errorf("reason %d has no name", r)
		}
	}
}

// K12, K13: a group declared twice keeps the entries of both headers, and
// only the last occurrence of a key counts.
func TestDuplicateGroupsAndKeys(t *testing.T) {
	m := buildWith("[a]\nx=1\n[b]\ny=2\n[a]\nx=3\nz=4\nk[fr]=5\n", newFake(true), goption.CharsetASCII)
	var got []string
	for _, g := range m.Groups {
		for _, e := range g.Entries {
			got = append(got, fmt.Sprintf("%s.%s=%s shadowed=%v last=%d", g.Name, e.Key, e.Value, e.Shadowed, e.LastLine))
		}
	}
	want := "a.x=3 shadowed=true last=6|a.x=3 shadowed=false last=6|a.z=4 shadowed=false last=7|a.k[fr]=5 shadowed=false last=0|b.y=2 shadowed=false last=4"
	if strings.Join(got, "|") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, "|"), want)
	}
}

// The fatal error lands on the key GOption failed on, even when a key that
// is not passed (a connection key) comes first.
func TestFatalAttributionSkipsUnpassedKeys(t *testing.T) {
	m := buildWith("[mydumper]\nhost=h\nbogus=1\n", newFake(false), goption.CharsetASCII)
	if got := reasons(m); got != "mydumper.host:connection-key mydumper.bogus:fatal-at-startup" {
		t.Errorf("reasons: %s", got)
	}
}

// lastOccurrences sorts instead of hashing; it must agree with a map.
func TestLastOccurrencesMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 200; round++ {
		var b strings.Builder
		for i := 0; i < 30; i++ {
			if rng.Intn(4) == 0 {
				fmt.Fprintf(&b, "[g%d]\n", rng.Intn(3))
				continue
			}
			key := fmt.Sprintf("k%d", rng.Intn(5))
			if rng.Intn(6) == 0 {
				key += "[fr]"
			}
			fmt.Fprintf(&b, "%s=v%d\n", key, i)
		}
		src := "[g0]\n" + b.String()
		kf := parse(src)
		lastLine, lastValue := lastOccurrences(kf, []string{"C"})
		type gk struct{ g, k string }
		wantLine, wantValue := map[gk]int{}, map[gk]string{}
		for _, e := range kf.Entries {
			k := gk{kf.Groups[e.Group].Name, e.Key}
			wantValue[k] = e.Value
			if e.Locale == "" {
				wantLine[k] = e.Line
			}
		}
		for i, e := range kf.Entries {
			k := gk{kf.Groups[e.Group].Name, e.Key}
			if lastLine[i] != wantLine[k] || lastValue[i] != wantValue[k] {
				t.Fatalf("round %d, line %d (%s): got %d %q, want %d %q\n%s", round, e.Line, e.Key, lastLine[i], lastValue[i], wantLine[k], wantValue[k], src)
			}
		}
	}
}

// F9: a masked column key starts with a backtick and contains a second one,
// possibly right after the first.
func TestIsMaskedColumn(t *testing.T) {
	for key, want := range map[string]bool{"``": true, "`a`": true, "`a": false, "`": false, "a`b`": false} {
		if IsMaskedColumn(key) != want {
			t.Errorf("IsMaskedColumn(%q) = %v", key, !want)
		}
	}
}
