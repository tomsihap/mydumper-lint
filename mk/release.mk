# Release engineering (design §12.3): goreleaser, pinned to the Docker image
# below, the same pattern mk/lint.mk uses for golangci-lint.
#
# `snapshot` also builds the multi-arch ghcr.io image (design §12.3,
# .goreleaser.yaml `dockers`), which needs a Docker daemon. The official
# goreleaser image already bundles a docker CLI, the buildx plugin and
# syft/cosign, so mounting the host's socket ("Docker outside of Docker", not
# Docker-in-Docker) is enough for it to build and tag images on the host.
#
# GORELEASER_VERSION: bump the tag and digest together, as in mk/lint.mk:
#   docker buildx imagetools inspect goreleaser/goreleaser:TAG --format '{{json .Manifest.Digest}}'

GORELEASER_VERSION := v2.18.2
GORELEASER_IMAGE := goreleaser/goreleaser:$(GORELEASER_VERSION)@sha256:7077423cf5ef643ff56a34b58f93c1364e927e5c3dfa470eeabc44cab1a9c72b

DOCKER ?= docker
# Keeps the Go build and module caches between runs, as LINT_CACHE_VOLUME does
# in mk/lint.mk (`docker volume rm` it to start cold).
RELEASE_CACHE_VOLUME ?= mydumper-lint-goreleaser-cache

# goreleaser reads git history (tags, the current commit) to resolve the
# version. Mounted at its own absolute path rather than remapped to /src, so
# that a linked git worktree's ".git" file — which points at an absolute host
# path outside $(CURDIR) — still resolves inside the container. In a plain
# (non-worktree) checkout this equals "$(CURDIR)/.git" and the mount is a
# harmless no-op alongside the one below.
GIT_COMMON_DIR := $(realpath $(shell git rev-parse --git-common-dir))

GORELEASER = $(DOCKER) run --rm \
	--volume "$(CURDIR):$(CURDIR)" --workdir "$(CURDIR)" \
	--volume "$(GIT_COMMON_DIR):$(GIT_COMMON_DIR):ro" \
	--volume /var/run/docker.sock:/var/run/docker.sock \
	--volume $(RELEASE_CACHE_VOLUME):/cache \
	--env GOCACHE=/cache/go-build \
	--env GOMODCACHE=/cache/mod \
	--env GOPATH=/cache/gopath \
	$(GORELEASER_IMAGE)

.PHONY: release-check snapshot

release-check: ## Validate .goreleaser.yaml (goreleaser check; no network needed)
	$(GORELEASER) check

# --snapshot skips every publish step (no GitHub release, no registry push)
# and fills in a placeholder version, so it needs no token. --skip=sign is
# separate: cosign's keyless signing needs a real Sigstore/OIDC identity
# (GitHub Actions' ambient token in release.yml); locally it would otherwise
# hang on an interactive device-code flow. Artifacts land in dist/ (gitignored).
snapshot: ## Build every release target and the ghcr.io image locally, without publishing (dist/)
	$(GORELEASER) release --snapshot --clean --skip=sign
