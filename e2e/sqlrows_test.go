//go:build e2e

package e2e

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// sqlRows extracts the rows of the INSERT statements of mydumper's SQL data
// files: every value tuple after "VALUES" (`VALUES(1,"a","b")` then
// `,(2,"c","d")` … `;`). A field is nil for NULL; quoted strings are decoded
// (backslash escapes, doubled quotes); other literals are kept as written.
func sqlRows(s string) ([][]*string, error) {
	var rows [][]*string
	for {
		i := strings.Index(s, "VALUES")
		if i < 0 {
			return rows, nil
		}
		s = s[i+len("VALUES"):]
		for {
			s = strings.TrimLeft(s, " \t\r\n")
			if !strings.HasPrefix(s, "(") {
				return nil, fmt.Errorf("value tuple expected at %.20q", s)
			}
			row, rest, err := sqlTuple(s[1:])
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
			s = strings.TrimLeft(rest, " \t\r\n")
			if !strings.HasPrefix(s, ",") {
				break // ";" ends the statement
			}
			s = s[1:]
		}
	}
}

// sqlTuple parses the fields of a tuple, after its "(", up to its ")".
func sqlTuple(s string) (row []*string, rest string, err error) {
	errUnterminated := errors.New("unterminated value tuple")
	for {
		s = strings.TrimLeft(s, " ")
		if s == "" {
			return nil, "", errUnterminated
		}
		var field *string
		if s[0] == '"' || s[0] == '\'' {
			v, after, err := sqlString(s)
			if err != nil {
				return nil, "", err
			}
			field, s = &v, after
		} else {
			j := strings.IndexAny(s, ",)")
			if j < 0 {
				return nil, "", errUnterminated
			}
			raw := strings.TrimSpace(s[:j])
			if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
				return nil, "", fmt.Errorf("invalid literal %q", raw)
			}
			if raw != "NULL" {
				field = &raw
			}
			s = s[j:]
		}
		row = append(row, field)
		s = strings.TrimLeft(s, " ")
		switch {
		case s == "":
			return nil, "", errUnterminated
		case s[0] == ')':
			return row, s[1:], nil
		case s[0] != ',':
			return nil, "", fmt.Errorf("',' or ')' expected at %.20q", s)
		}
		s = s[1:]
	}
}

// sqlString decodes a quoted SQL literal at the start of s.
func sqlString(s string) (value, rest string, err error) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case 'Z':
				b.WriteByte(26)
			default:
				b.WriteByte(s[i])
			}
		case c == q && i+1 < len(s) && s[i+1] == q:
			b.WriteByte(q)
			i++
		case c == q:
			return b.String(), s[i+1:], nil
		default:
			b.WriteByte(c)
		}
	}
	return "", "", errors.New("unterminated string literal")
}

// classifyColumn says what the rows hold in a seeded column: plaintext (the
// seeded values), masked (other non-empty values), empty, null, or a mix.
func classifyColumn(rows [][]*string, col seededColumn) string {
	var seeded, empty, null int
	var other []string
	for _, row := range rows {
		if col.Index >= len(row) {
			return fmt.Sprintf("unexpected row of %d fields", len(row))
		}
		switch v := row[col.Index]; {
		case v == nil:
			null++
		case *v == "":
			empty++
		case slices.Contains(col.Values, *v):
			seeded++
		default:
			other = append(other, strconv.Quote(*v))
		}
	}
	n := len(rows)
	switch {
	case n == 0:
		return "no rows"
	case seeded == n && n == len(col.Values):
		return "plaintext"
	case len(other) == n:
		return "masked (" + strings.Join(other, ", ") + ")"
	case empty == n:
		return `empty ("" in every row)`
	case null == n:
		return "null (NULL in every row)"
	}
	return fmt.Sprintf("mixed (%d rows: %d seeded, %d other, %d empty, %d NULL)", n, seeded, len(other), empty, null)
}

// TestSQLRows checks the data-file parser on mydumper's format. No Docker.
func TestSQLRows(t *testing.T) {
	data := "/*!40101 SET NAMES binary*/;\n/*!40103 SET TIME_ZONE='+00:00' */;\n" +
		"INSERT INTO `users` VALUES(1,\"alice@e2e.example\",\"Alice\")\n" +
		",(2,\"\",NULL)\n,(3,'it''s','a \\\"q\\\", b)')\n;\n"
	rows, err := sqlRows(data)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		var fields []string
		for _, f := range r {
			if f == nil {
				fields = append(fields, "NULL")
			} else {
				fields = append(fields, strconv.Quote(*f))
			}
		}
		got = append(got, strings.Join(fields, " "))
	}
	want := []string{
		`"1" "alice@e2e.example" "Alice"`,
		`"2" "" NULL`,
		`"3" "it's" "a \"q\", b)"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("sqlRows:\ngot  %q\nwant %q", got, want)
	}
	col := seededColumns["app.users.email"]
	for _, c := range []struct {
		data, want string
	}{
		{"VALUES(1,\"alice@e2e.example\",\"A\"),(2,\"bob@e2e.example\",\"B\"),(3,\"zoe@e2e.example\",\"Z\");", "plaintext"},
		{"VALUES(1,\"x\",\"A\"),(2,\"y\",\"B\"),(3,\"z\",\"Z\");", `masked ("x", "y", "z")`},
		{"VALUES(1,\"\",\"A\"),(2,\"\",\"B\"),(3,\"\",\"Z\");", `empty ("" in every row)`},
		{"VALUES(1,NULL,\"A\"),(2,NULL,\"B\"),(3,NULL,\"Z\");", "null (NULL in every row)"},
		{"VALUES(1,\"alice@e2e.example\",\"A\"),(2,\"y\",\"B\");", "mixed (2 rows: 1 seeded, 1 other, 0 empty, 0 NULL)"},
		{"", "no rows"},
	} {
		rows, err := sqlRows(c.data)
		if err != nil {
			t.Fatal(err)
		}
		if got := classifyColumn(rows, col); got != c.want {
			t.Errorf("classifyColumn(%q) = %q, want %q", c.data, got, c.want)
		}
	}
	for _, bad := range []string{"VALUES(1,\"a", "VALUES 1", "VALUES(1 2)"} {
		if _, err := sqlRows(bad); err == nil {
			t.Errorf("sqlRows(%q): no error", bad)
		}
	}
}
