package rules

// commentStart returns the offset in value of an end-of-line style comment
// (" #" or "\t#"), or -1. With sql set, a '#' inside '...', "..." or `...`
// quotes does not count: quotes may be doubled or escaped with a backslash.
func commentStart(value string, sql bool) int {
	var quote byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case quote != 0:
			switch {
			case c == '\\' && quote != '`':
				i++
			case c == quote && i+1 < len(value) && value[i+1] == quote:
				i++
			case c == quote:
				quote = 0
			}
		case sql && (c == '\'' || c == '"' || c == '`'):
			quote = c
		case c == '#' && i > 0 && (value[i-1] == ' ' || value[i-1] == '\t'):
			return i
		}
	}
	return -1
}
