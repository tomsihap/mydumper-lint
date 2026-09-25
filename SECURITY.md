# Security policy

## Supported versions

Security fixes are released for the latest minor release only, as a new patch
release. Older releases are not patched: upgrade to the latest release.

| Version | Supported |
| --- | --- |
| Latest minor release (for example `0.3.x` when `0.3` is the newest minor) | Yes |
| Older minor releases | No |
| `main`, until the first release | Yes |

## Reporting a vulnerability

Do not open a public issue, discussion or pull request for a vulnerability.

Report it privately through GitHub's private vulnerability reporting: open the
repository's **Security** tab and choose **Report a vulnerability**, or go
directly to <https://github.com/tomsihap/mydumper-lint/security/advisories/new>.
Only you and the maintainers can see the report.

## What to include

- The mydumper-lint version (`mydumper-lint version`), how you installed it
  (release binary, `go install`, Docker image, pre-commit hook, GitHub Action),
  and your operating system and architecture.
- The target mydumper version (`--mydumper-version`, or `mydumper-version` in
  `.mydumper-lint.yaml`).
- The exact command line, and your `.mydumper-lint.yaml` if you use one.
- The smallest input that reproduces the problem. Bytes matter (carriage
  returns, NUL bytes, a byte order mark, a missing final newline), so attach
  the file or give a hex dump (`xxd file.cnf`) rather than pasting it as text.
  **Remove real passwords, host names, user names and schema names first.**
- What happens (output, stack trace, files written) and the impact you expect,
  for example a file written outside the linted files, a hang, or a secret
  printed in a report.
- Whether the problem is already known to others or public.

## What happens next

These are targets, on a best-effort basis:

- acknowledgement within 3 working days;
- a first assessment (confirmed or not, severity) within 10 working days;
- a fixed release within 30 days for critical and high severity issues, and
  within 90 days otherwise.

The fix is published with a GitHub security advisory (and a CVE when
warranted) that credits the reporter, unless they prefer to stay anonymous. We
coordinate the disclosure date with you. We will not take action against
good-faith research that follows this policy.

## Scope

mydumper-lint treats every file it reads as untrusted: in CI, the
configuration files and `.mydumper-lint.yaml` come from pull requests. In
scope:

- Crashes, panics, hangs, or excessive CPU or memory use on crafted input,
  including catastrophic backtracking in regular expressions taken from the
  input.
- `--fix` writing anything other than the files it was asked to fix: path
  traversal, following a symbolic link to another file, clobbering unrelated
  files, a non-atomic write that can lose data, or changed file permissions.
- Opening files whose paths come from the contents of a linted file, such as
  `!include` directives.
- Secrets from a linted file, such as `[client]` passwords, disclosed in the
  output, logs or reports (text, JSON, SARIF, GitHub annotations, JUnit,
  GitLab Code Quality).
- Output injection from file contents: terminal escape sequences, or GitHub
  Actions workflow commands smuggled through `--format github`.
- The release artifacts and their signatures and attestations, the Docker
  image, the GitHub Action and the pre-commit hooks.

Out of scope:

- Vulnerabilities in mydumper, myloader, MySQL or GLib: report them to those
  projects.
- The test-only oracle in `tools/oracle/` and the end-to-end test harness,
  which are never shipped.
- Wrong lint results (false positives or false negatives) without a security
  impact: they are bugs, so open an issue, after removing any secret.
