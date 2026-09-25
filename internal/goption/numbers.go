package goption

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// parseInt is GLib's parse_int (bits 32: strtol, then a check that the value
// fits a gint) and parse_int64 (bits 64: g_ascii_strtoll, which glibc builds
// run as strtoll_l in the C locale). Both use base 0. It returns GLib's
// message on failure.
func parseInt(s, name string, bits int) (int64, string) {
	v, end, erange := strtoll(s)
	if s == "" || end != len(s) {
		return 0, "Cannot parse integer value “" + s + "” for " + name
	}
	if erange || (bits == 32 && (v < math.MinInt32 || v > math.MaxInt32)) {
		return 0, "Integer value “" + s + "” for " + name + " out of range"
	}
	return v, ""
}

// isCSpace is isspace in the C locale.
func isCSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// strtoll is C's strtoll(s, &end, 0) for a 64-bit long: leading whitespace,
// an optional sign, then "0x" (hexadecimal), "0" (octal) or decimal digits.
// end is the index where conversion stopped (0 when nothing was converted);
// erange reports an overflow, the value then being clamped.
func strtoll(s string) (v int64, end int, erange bool) {
	i := 0
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	base := uint64(10)
	switch {
	case i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X'):
		if i+2 < len(s) && digitValue(s[i+2]) < 16 {
			base, i = 16, i+2
		} else {
			return 0, i + 1, false // "0x" without digits: only the "0" converts
		}
	case i < len(s) && s[i] == '0':
		base = 8
	}
	limit := uint64(math.MaxInt64)
	if neg {
		limit++
	}
	start := i
	var acc uint64
	over := false
	for ; i < len(s); i++ {
		d := digitValue(s[i])
		if d >= base {
			break
		}
		if !over && (acc > (limit-d)/base) {
			over = true
		}
		if !over {
			acc = acc*base + d
		}
	}
	if i == start {
		return 0, 0, false
	}
	switch {
	case over && neg:
		return math.MinInt64, i, true
	case over:
		return math.MaxInt64, i, true
	case neg:
		return -int64(acc-1) - 1, i, false //nolint:gosec // G115: acc ≤ 2^63 checked above
	}
	return int64(acc), i, false
}

// digitValue is the value of an ASCII digit or letter in bases up to 36, or
// 36 when c is neither.
func digitValue(c byte) uint64 {
	switch {
	case c >= '0' && c <= '9':
		return uint64(c - '0')
	case c >= 'a' && c <= 'z':
		return uint64(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return uint64(c-'A') + 10
	}
	return 36
}

// parseDouble is GLib's parse_double (g_strtod). No mydumper option is a
// double; this follows C's strtod for the usual forms (decimal and
// hexadecimal numbers, inf, infinity and nan) in the C locale.
func parseDouble(s, name string) (float64, string) {
	v, end, erange := strtod(s)
	if s == "" || end != len(s) {
		return 0, "Cannot parse double value “" + s + "” for " + name
	}
	if erange {
		return 0, "Double value “" + s + "” for " + name + " out of range"
	}
	return v, ""
}

// strtod is C's strtod(s, &end) in the C locale.
func strtod(s string) (v float64, end int, erange bool) {
	i := 0
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	signAt := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	neg := signAt < i && s[signAt] == '-'
	rest := strings.ToLower(s[i:])
	switch {
	case strings.HasPrefix(rest, "infinity"):
		return math.Inf(sign(neg)), i + len("infinity"), false
	case strings.HasPrefix(rest, "inf"):
		return math.Inf(sign(neg)), i + len("inf"), false
	case strings.HasPrefix(rest, "nan"):
		j := i + 3
		if j < len(s) && s[j] == '(' {
			k := j + 1
			for k < len(s) && (digitValue(s[k]) < 36 || s[k] == '_') {
				k++
			}
			if k < len(s) && s[k] == ')' {
				j = k + 1
			}
		}
		return math.NaN(), j, false
	}
	hex := strings.HasPrefix(rest, "0x")
	base, expChar := uint64(10), byte('e')
	j := i
	if hex {
		base, expChar, j = 16, 'p', i+2
	}
	digits := 0
	nonzero := false
	for j < len(s) && digitValue(s[j]) < base {
		nonzero = nonzero || s[j] != '0'
		digits++
		j++
	}
	if j < len(s) && s[j] == '.' {
		j++
		for j < len(s) && digitValue(s[j]) < base {
			nonzero = nonzero || s[j] != '0'
			digits++
			j++
		}
	}
	if digits == 0 {
		if hex {
			return 0, i + 1, false // "0x" without digits: the "0" converts
		}
		return 0, 0, false
	}
	mantissaEnd := j
	if j < len(s) && (s[j]|0x20) == expChar {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		if k < len(s) && s[k] >= '0' && s[k] <= '9' {
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			j = k
		}
	}
	text := s[signAt:j]
	if hex && j == mantissaEnd {
		text += "p0" // Go requires the binary exponent of hexadecimal floats
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil && !isRange(err) {
		return 0, 0, false
	}
	tiny := f != 0 && math.Abs(f) < 0x1p-1022
	return f, j, isRange(err) || tiny || (f == 0 && nonzero)
}

func isRange(err error) bool {
	return errors.Is(err, strconv.ErrRange)
}

func sign(neg bool) int {
	if neg {
		return -1
	}
	return 1
}
