package report

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"plain ASCII: a=b [c]", "plain ASCII: a=b [c]"},
		{"tab\there", "tab\U00002409here"},
		{"nul\x00cr\rlf\n", "nul\U00002400cr\U0000240dlf\U0000240a"},
		{"esc\x1b[31m del\x7f", "esc\U0000241b[31m del\U00002421"},
		{"bad \xff\xfe byte", `bad \xff\xfe byte`},
		{"truncated \xe6\x97", `truncated \xe6\x97`},
		{"bom \xef\xbb\xbf zwsp \U0000200b rlo \U0000202e", `bom \u{feff} zwsp \u{200b} rlo \u{202e}`},
		{"c1 \xc2\x85 \xc2\x9b", `c1 \u{85} \u{9b}`},
		{"private \U000f0000", `private \u{f0000}`},
		{"caf\xc3\xa9 e\U00000301 \U000065e5\U0000672c \U0001f600 \U0000fffd", "caf\xc3\xa9 e\U00000301 \U000065e5\U0000672c \U0001f600 \U0000fffd"},
	}
	for _, tt := range tests {
		if got := sanitize(tt.in); got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRuneWidth(t *testing.T) {
	tests := map[rune]int{
		'a': 1, 0xe9: 1, 0x301: 0, 0x20dd: 0, 0x1100: 2, 0x303e: 2, 0x303f: 1,
		0x65e5: 2, 0xac00: 2, 0xff21: 2, 0xff61: 1, 0x1f600: 2, 0x20000: 2, 0x3fffd: 2, 0x40000: 1,
	}
	for r, want := range tests {
		if got := runeWidth(r); got != want {
			t.Errorf("runeWidth(%U) = %d, want %d", r, got, want)
		}
	}
}

func TestMakeExcerpt(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		from, to int
		want     excerpt
	}{
		{"ascii", "key=value", 4, 9, excerpt{"key=value", 4, 5}},
		{"empty span", "key=value", 3, 3, excerpt{"key=value", 3, 1}},
		{"end of line", "key", 3, 4, excerpt{"key", 3, 1}},
		{"past the line", "key", 3, 99, excerpt{"key", 3, 1}},
		{"empty line", "", 0, 1, excerpt{"", 0, 1}},
		{"tab before", "\tk=v", 1, 2, excerpt{"    k=v", 4, 1}},
		{"tab inside", "k=\tv", 2, 3, excerpt{"k=    v", 2, 4}},
		{"trailing whitespace trimmed", "k=v \t", 3, 5, excerpt{"k=v", 3, 5}},
		{"wide characters", "n=\xe6\x97\xa5\xe6\x9c\xac!", 2, 8, excerpt{"n=\U000065e5\U0000672c!", 2, 4}},
		{"combining mark", "e\xcc\x81=x", 3, 4, excerpt{"e\U00000301=x", 1, 1}},
		{"invalid byte", "a\xffb", 1, 2, excerpt{`a\xffb`, 1, 4}},
		{"control characters", "\x00\r", 0, 2, excerpt{"\U00002400\U0000240d", 0, 2}},
		{"bom", "\xef\xbb\xbf[g]", 3, 6, excerpt{`\u{feff}[g]`, 8, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := makeExcerpt([]byte(tt.line), tt.from, tt.to); got != tt.want {
				t.Errorf("makeExcerpt(%q, %d, %d) = %+v, want %+v", tt.line, tt.from, tt.to, got, tt.want)
			}
		})
	}
}
