# Release image for mydumper-lint (design §12.3): ghcr.io/tomsihap/mydumper-lint.
#
# Used by GoReleaser (.goreleaser.yaml, dockers), never built standalone: the
# build context GoReleaser hands this file contains only the static binary it
# already built for the target GOOS/GOARCH, named "mydumper-lint" (see
# `builds.binary`). This Dockerfile must not invoke `go build` itself.
FROM scratch

LABEL org.opencontainers.image.title="mydumper-lint" \
      org.opencontainers.image.description="Static analysis for mydumper and myloader configuration files" \
      org.opencontainers.image.url="https://github.com/tomsihap/mydumper-lint" \
      org.opencontainers.image.source="https://github.com/tomsihap/mydumper-lint" \
      org.opencontainers.image.documentation="https://github.com/tomsihap/mydumper-lint#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="mydumper-lint authors"
# org.opencontainers.image.version, .revision and .created are set per build
# from --label build flags in .goreleaser.yaml, since they vary per release.

COPY mydumper-lint /mydumper-lint

# scratch has no /etc/passwd: a numeric UID needs no user database entry, and
# it is what Kubernetes' runAsNonRoot / runAsUser checks compare against.
USER 65532:65532

ENTRYPOINT ["/mydumper-lint"]
