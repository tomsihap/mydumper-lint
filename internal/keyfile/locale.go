package keyfile

import "strings"

// Components of a locale name, as in GLib's explode_locale.
const (
	compCodeset   = 1 << 0
	compTerritory = 1 << 1
	compModifier  = 1 << 2
)

// LanguageNames emulates g_get_language_names(): the language list GKeyFile
// uses to decide which localized keys it keeps (K14). It depends on the
// environment of the mydumper process: LANGUAGE, then LC_ALL, LC_MESSAGES and
// LANG; "C" is always appended. (GLib may also resolve locale.alias entries;
// that is not needed for the locales mydumper users run with.)
func LanguageNames(getenv func(string) string) []string {
	value := ""
	for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(k); v != "" {
			value = v
			break
		}
	}
	if value == "" {
		value = "C"
	}
	var out []string
	for _, lang := range strings.Split(value, ":") {
		out = append(out, localeVariants(lang)...)
	}
	return append(out, "C")
}

// localeVariants lists the variants of a locale from most to least specific,
// in GLib's order (append_locale_variants).
func localeVariants(locale string) []string {
	language, territory, codeset, modifier, mask := explodeLocale(locale)
	var out []string
	for j := 0; j <= mask; j++ {
		i := mask - j
		if i&^mask != 0 {
			continue
		}
		v := language
		if i&compTerritory != 0 {
			v += territory
		}
		if i&compCodeset != 0 {
			v += codeset
		}
		if i&compModifier != 0 {
			v += modifier
		}
		out = append(out, v)
	}
	return out
}

// explodeLocale splits "language_TERRITORY.codeset@modifier".
func explodeLocale(locale string) (language, territory, codeset, modifier string, mask int) {
	from := func(start int, c byte) int {
		if i := strings.IndexByte(locale[start:], c); i >= 0 {
			return start + i
		}
		return -1
	}
	uscore := strings.IndexByte(locale, '_')
	dot := from(max(uscore, 0), '.')
	atStart := max(uscore, 0)
	if dot >= 0 {
		atStart = dot
	}
	at := from(atStart, '@')
	if at >= 0 {
		mask |= compModifier
		modifier = locale[at:]
	} else {
		at = len(locale)
	}
	if dot >= 0 {
		mask |= compCodeset
		codeset = locale[dot:at]
	} else {
		dot = at
	}
	if uscore >= 0 {
		mask |= compTerritory
		territory = locale[uscore:dot]
	} else {
		uscore = dot
	}
	return locale[:uscore], territory, codeset, modifier, mask
}

// IsInterestingLocale is g_key_file_locale_is_interesting: whether a
// localized key is kept, comparing whole names ASCII case-insensitively.
func IsInterestingLocale(locale string, languages []string) bool {
	for _, lang := range languages {
		if len(lang) == len(locale) && asciiEqualFold(lang, locale) {
			return true
		}
	}
	return false
}

func asciiEqualFold(a, b string) bool {
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
