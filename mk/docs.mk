# Documentation generated from the code (design §12.5): docs/rules/, the README
# rule and version tables, docs/versions.md and schemas/config.v1.json.

.PHONY: docs docs-check

docs: ## Regenerate the documentation derived from the code
	$(GO) run ./tools/gendocs

docs-check: ## Fail if the generated documentation is out of date
	$(GO) run ./tools/gendocs -check
