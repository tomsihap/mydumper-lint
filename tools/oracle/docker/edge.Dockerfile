# GLib oracle on Fedora Rawhide: the newest GLib on glibc, like the other two
# images, so that only GLib varies across the matrix. Test-only,
# GPL-3.0-or-later: see tools/oracle/README.md.
# Build: make oracle-images (build context: tools/oracle).

# Base image: fedora:rawhide, pinned by digest (Dependabot bumps it).
FROM fedora:rawhide@sha256:a9bab18d01cf2c2cf62f3e79c72623405bce14ac062995cc6651a3073c802e41 AS build
RUN dnf -y install gcc glib2-devel pkgconf-pkg-config \
 && dnf clean all
COPY oracle.c /src/oracle.c
RUN gcc -O2 -Wall -Wextra -o /oracle /src/oracle.c $(pkg-config --cflags --libs glib-2.0) \
 && pkg-config --modversion glib-2.0 > /glib-version

# Base image: fedora:rawhide, same digest as above.
FROM fedora:rawhide@sha256:a9bab18d01cf2c2cf62f3e79c72623405bce14ac062995cc6651a3073c802e41
RUN dnf -y install glib2 \
 && dnf -y upgrade glib2 \
 && dnf clean all
COPY --from=build /oracle /usr/local/bin/oracle
# The runtime GLib must be the one the oracle was built against.
RUN --mount=type=bind,from=build,source=/glib-version,target=/tmp/glib-version \
    test "$(oracle --glib-version)" = "$(cat /tmp/glib-version)"
LABEL org.opencontainers.image.title="mydumper-lint GLib oracle (Fedora Rawhide)" \
      org.opencontainers.image.description="Test-only GLib oracle for mydumper-lint; never linked into its binary" \
      org.opencontainers.image.licenses="GPL-3.0-or-later"
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/oracle"]
