<!--
Thanks for contributing! The title of this pull request must follow
Conventional Commits, for example "feat(rules): add MDL305" or
"fix(keyfile): reject NUL bytes in group names": it ends up in the changelog.
-->

## What and why

<!-- What does this change, and why? Link the issue it resolves: "Closes #123". -->

## How it was tested

<!-- Unit tests, golden txtar cases, oracle cases, e2e scenarios, manual runs. -->

## Checklist

- [ ] The title follows [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `test:`, `ci:`, `chore:`…; `!` marks a breaking change).
- [ ] Tests cover every behavior change.
- [ ] `make check-all` passes locally.
- [ ] If rules changed, the generated rule documentation (`docs/rules/`) is regenerated and committed.
- [ ] Fixtures, logs and screenshots contain no passwords, host names or other secrets.
