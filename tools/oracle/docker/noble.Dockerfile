# GLib oracle on Ubuntu 24.04 "noble" (GLib 2.80.x), the base of the
# distribution packages. Test-only, GPL-3.0-or-later: see tools/oracle/README.md.
# Build: make oracle-images (build context: tools/oracle).

# Base image: ubuntu:24.04, pinned by digest (Dependabot bumps it).
FROM ubuntu:24.04@sha256:008173c23f95b170204355c12626cb5a965d779a7e1283b09e9cffbb1bf33ca3 AS build
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends gcc libc6-dev libglib2.0-dev pkgconf \
 && rm -rf /var/lib/apt/lists/*
COPY oracle.c /src/oracle.c
RUN gcc -O2 -Wall -Wextra -o /oracle /src/oracle.c $(pkg-config --cflags --libs glib-2.0) \
 && pkg-config --modversion glib-2.0 > /glib-version

# Base image: ubuntu:24.04, same digest as above.
FROM ubuntu:24.04@sha256:008173c23f95b170204355c12626cb5a965d779a7e1283b09e9cffbb1bf33ca3
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libglib2.0-0t64 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /oracle /usr/local/bin/oracle
# The runtime GLib must be the one the oracle was built against.
RUN --mount=type=bind,from=build,source=/glib-version,target=/tmp/glib-version \
    test "$(oracle --glib-version)" = "$(cat /tmp/glib-version)"
LABEL org.opencontainers.image.title="mydumper-lint GLib oracle (Ubuntu 24.04)" \
      org.opencontainers.image.description="Test-only GLib oracle for mydumper-lint; never linked into its binary" \
      org.opencontainers.image.licenses="GPL-3.0-or-later"
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/oracle"]
