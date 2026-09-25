package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

// checkFlagNames lists the check flags for completion scripts.
func checkFlagNames() []string {
	var names []string
	(&checkCmd{}).flags().VisitAll(func(f *flag.Flag) { names = append(names, "--"+f.Name) })
	return names
}

func commandNames() []string {
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.name
	}
	return names
}

func runCompletion(args []string, e *env) int {
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	pos, err := parse(fs, args)
	if errors.Is(err, errHelp) || (err == nil && len(pos) != 1) {
		fmt.Fprint(e.stdout, "Usage: mydumper-lint completion bash|zsh|fish|powershell\n\n"+
			"Print a completion script, for example:\n  mydumper-lint completion bash > /etc/bash_completion.d/mydumper-lint\n"+
			"  mydumper-lint completion zsh > \"${fpath[1]}/_mydumper-lint\"\n"+
			"  mydumper-lint completion fish > ~/.config/fish/completions/mydumper-lint.fish\n")
		if err == nil && len(pos) != 1 {
			return ExitError
		}
		return ExitOK
	}
	if err != nil {
		return usageError(e, "completion", err)
	}
	cmds := strings.Join(commandNames(), " ")
	flags := strings.Join(checkFlagNames(), " ")
	switch pos[0] {
	case "bash":
		fmt.Fprintf(e.stdout, `# bash completion for mydumper-lint
_mydumper_lint() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    if [[ ${COMP_CWORD} -eq 1 ]]; then
        COMPREPLY=($(compgen -W "%s" -- "$cur") $(compgen -f -- "$cur"))
        return
    fi
    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "%s" -- "$cur"))
    else
        COMPREPLY=($(compgen -f -- "$cur"))
    fi
}
complete -o filenames -F _mydumper_lint mydumper-lint
`, cmds, flags)
	case "zsh":
		fmt.Fprintf(e.stdout, `#compdef mydumper-lint
_mydumper_lint() {
    if (( CURRENT == 2 )); then
        _alternative 'commands:command:(%s)' 'files:file:_files'
    elif [[ "$PREFIX" == -* ]]; then
        compadd -- %s
    else
        _files
    fi
}
_mydumper_lint "$@"
`, cmds, flags)
	case "fish":
		var b strings.Builder
		b.WriteString("# fish completion for mydumper-lint\n")
		for _, c := range commands {
			fmt.Fprintf(&b, "complete -c mydumper-lint -n __fish_use_subcommand -a %s -d %q\n", c.name, c.summary)
		}
		for _, f := range checkFlagNames() {
			fmt.Fprintf(&b, "complete -c mydumper-lint -l %s\n", strings.TrimPrefix(f, "--"))
		}
		fmt.Fprint(e.stdout, b.String())
	case "powershell":
		fmt.Fprintf(e.stdout, `# PowerShell completion for mydumper-lint
Register-ArgumentCompleter -Native -CommandName mydumper-lint -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $words = @(%s) + @(%s)
    $words | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`, quoteList(commandNames()), quoteList(checkFlagNames()))
	default:
		return usageError(e, "completion", fmt.Errorf("unsupported shell %q (bash, zsh, fish or powershell)", pos[0]))
	}
	return ExitOK
}

func quoteList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "'" + s + "'"
	}
	return strings.Join(q, ", ")
}
