# GLib oracle on AlmaLinux 9 (GLib 2.68.x), the base of the official mydumper
# images. Test-only, GPL-3.0-or-later: see tools/oracle/README.md.
# Build: make oracle-images (build context: tools/oracle).

# Base image: almalinux:9, pinned by digest (Dependabot bumps it).
FROM almalinux:10@sha256:1057738702313e6ee452cdb17bc1431542c467be9a4e2f4da3b8e551b0ebb9677 AS build
RUN dnf -y install gcc glib2-devel pkgconf-pkg-config \
 && dnf clean all
COPY oracle.c /src/oracle.c
RUN gcc -O2 -Wall -Wextra -o /oracle /src/oracle.c $(pkg-config --cflags --libs glib-2.0) \
 && pkg-config --modversion glib-2.0 > /glib-version

# Base image: almalinux:9, same digest as above.
FROM almalinux:10@sha256:1057738702313e6ee452cdb17bc1431542c467be9a4e2f4da3b8e551b0ebb9677
RUN dnf -y install glib2 \
 && dnf -y upgrade glib2 \
 && dnf clean all
COPY --from=build /oracle /usr/local/bin/oracle
# The runtime GLib must be the one the oracle was built against.
RUN --mount=type=bind,from=build,source=/glib-version,target=/tmp/glib-version \
    test "$(oracle --glib-version)" = "$(cat /tmp/glib-version)"
LABEL org.opencontainers.image.title="mydumper-lint GLib oracle (AlmaLinux 9)" \
      org.opencontainers.image.description="Test-only GLib oracle for mydumper-lint; never linked into its binary" \
      org.opencontainers.image.licenses="GPL-3.0-or-later"
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/oracle"]
