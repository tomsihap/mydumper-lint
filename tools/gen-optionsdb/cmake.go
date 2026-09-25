package main

import (
	"fmt"
	"strings"
)

// cmakeExecutables returns the source list of every add_executable target of
// a CMakeLists.txt. It understands set(), list(APPEND), target_sources() and
// ${VAR} expansion, and ignores control flow: mydumper's build lists its
// sources unconditionally.
func cmakeExecutables(src string) (map[string][]string, error) {
	cmds, err := cmakeCommands(src)
	if err != nil {
		return nil, err
	}
	vars := map[string][]string{}
	targets := map[string][]string{}
	expand := func(args []cmakeArg) []string {
		var out []string
		for _, a := range args {
			v := a.text
			for {
				i := strings.Index(v, "${")
				if i < 0 {
					break
				}
				j := strings.IndexByte(v[i:], '}')
				if j < 0 {
					break
				}
				v = v[:i] + strings.Join(vars[v[i+2:i+j]], ";") + v[i+j+1:]
			}
			if a.quoted {
				out = append(out, v)
				continue
			}
			for _, p := range strings.Split(v, ";") {
				if p != "" {
					out = append(out, p)
				}
			}
		}
		return out
	}
	for _, c := range cmds {
		args := expand(c.args)
		switch strings.ToLower(c.name) {
		case "set":
			if len(args) >= 1 {
				vars[args[0]] = args[1:]
			}
		case "list":
			if len(args) >= 2 && strings.EqualFold(args[0], "APPEND") {
				vars[args[1]] = append(vars[args[1]], args[2:]...)
			}
		case "add_executable":
			if len(args) >= 1 {
				var srcs []string
				for _, a := range args[1:] {
					switch a {
					case "WIN32", "MACOSX_BUNDLE", "EXCLUDE_FROM_ALL":
						continue
					}
					srcs = append(srcs, a)
				}
				targets[args[0]] = append(targets[args[0]], srcs...)
			}
		case "target_sources":
			if len(args) >= 1 {
				for _, a := range args[1:] {
					switch a {
					case "PRIVATE", "PUBLIC", "INTERFACE":
						continue
					}
					targets[args[0]] = append(targets[args[0]], a)
				}
			}
		}
	}
	return targets, nil
}

type cmakeArg struct {
	text   string
	quoted bool
}

type cmakeCommand struct {
	name string
	args []cmakeArg
	line int
}

// cmakeCommands splits a CMake script into commands.
func cmakeCommands(src string) ([]cmakeCommand, error) {
	var out []cmakeCommand
	line := 1
	i := 0
	skipSpace := func() {
		for i < len(src) {
			switch c := src[i]; c {
			case '\n':
				line++
				i++
			case ' ', '\t', '\r':
				i++
			case '#':
				for i < len(src) && src[i] != '\n' {
					i++
				}
			default:
				return
			}
		}
	}
	for {
		skipSpace()
		if i >= len(src) {
			return out, nil
		}
		start := i
		for i < len(src) && isIdentChar(src[i]) {
			i++
		}
		if start == i {
			return nil, fmt.Errorf("line %d: unexpected %q", line, src[i])
		}
		cmd := cmakeCommand{name: src[start:i], line: line}
		skipSpace()
		if i >= len(src) || src[i] != '(' {
			return nil, fmt.Errorf("line %d: %s without '('", line, cmd.name)
		}
		i++
		depth := 0
		for {
			skipSpace()
			if i >= len(src) {
				return nil, fmt.Errorf("line %d: unterminated %s(", cmd.line, cmd.name)
			}
			c := src[i]
			if c == ')' {
				i++
				if depth == 0 {
					break
				}
				depth--
				continue
			}
			if c == '(' {
				depth++
				i++
				continue
			}
			if c == '"' {
				i++
				var sb strings.Builder
				for i < len(src) && src[i] != '"' {
					if src[i] == '\\' && i+1 < len(src) {
						i++
					}
					if src[i] == '\n' {
						line++
					}
					sb.WriteByte(src[i])
					i++
				}
				if i >= len(src) {
					return nil, fmt.Errorf("line %d: unterminated string", line)
				}
				i++
				cmd.args = append(cmd.args, cmakeArg{text: sb.String(), quoted: true})
				continue
			}
			start := i
			for i < len(src) && !strings.ContainsRune(" \t\r\n()#\"", rune(src[i])) {
				i++
			}
			cmd.args = append(cmd.args, cmakeArg{text: src[start:i]})
		}
		out = append(out, cmd)
	}
}
