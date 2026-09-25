# Knowledge base of mydumper versions (design §5): internal/optionsdb/data/optionsdb.json.
# The generator caches API answers and source archives in .cache/gen-optionsdb/.
# GITHUB_TOKEN is optional (three API calls per run); `gh auth token` is used when available.

GEN_OPTIONSDB := $(GO) run ./tools/gen-optionsdb
GH_TOKEN_ENV := GITHUB_TOKEN="$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null || true)}"

.PHONY: gen-optionsdb gen-optionsdb-offline verify-images

gen-optionsdb: ## Regenerate the mydumper knowledge base from upstream (network)
	$(GH_TOKEN_ENV) $(GEN_OPTIONSDB)

gen-optionsdb-offline: ## Regenerate the mydumper knowledge base from the local cache only
	$(GEN_OPTIONSDB) -offline

verify-images: ## Cross-check every version against its official Docker image (pulls ~100 MB each)
	$(GH_TOKEN_ENV) $(GEN_OPTIONSDB) -verify-images
