# syntax=docker/dockerfile:1

# ladder-bench in a container, with the one thing that is hard to get: an ffmpeg
# built with libvmaf.
#
#   docker build -t ladder-bench .
#   docker run --rm -v "$PWD:/work" ladder-bench doctor
#
# See docs/docker.md for the flags that matter (bind mounts, --user, the work
# dir) and why they matter.

# --- ffmpeg with libvmaf -----------------------------------------------------
#
# libvmaf is a build-time option and no distro package enables it: Debian 12
# (ffmpeg 5.1), Debian 13 (7.1), Ubuntu 24.04 (6.1) and Alpine 3.21 (6.1) were
# all checked and all build ffmpeg without it. Installing `ffmpeg` from a
# package manager would produce an image that passes every test except the one
# that measures anything.
#
# So the binaries come from a static multi-arch build that has libvmaf, x264,
# x265, SVT-AV1 and VP9, pinned by **manifest list digest**: a rebuild of this
# image measures with exactly the same ffmpeg, and it stays multi-arch because
# the digest is the list's, not one platform's. Bump it deliberately — a new
# ffmpeg can move the numbers, which is the sort of change that belongs in a
# commit message.
FROM mwader/static-ffmpeg:7.1@sha256:a8090df5f5608daef387e1b2e93b98aaacb4d92153ad904e7d715c725724fca4 AS ffmpeg

# --- build -------------------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src
# The module graph changes far less often than the code, so it gets its own
# layer and its own cache.
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# VERSION is what `ladder-bench version` reports, and what a report carries in
# its header. Left as "docker" for a local build; CI passes the tag.
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/ladder-bench ./cmd/ladder-bench

# --- runtime -----------------------------------------------------------------
FROM alpine:3.21

ARG VERSION=docker
LABEL org.opencontainers.image.title="ladder-bench" \
      org.opencontainers.image.description="Measure your ABR encoding ladder instead of inheriting it." \
      org.opencontainers.image.source="https://github.com/Allan-Nava/ladder-bench" \
      org.opencontainers.image.documentation="https://allan-nava.github.io/ladder-bench/docker/" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

# The ffmpeg binaries are static, so they need nothing from this base beyond a
# place to live. Alpine rather than scratch on purpose: a shell is what turns
# "the container did something odd" into a session you can look around in.
COPY --from=ffmpeg /ffmpeg /ffprobe /usr/local/bin/
COPY --from=build /out/ladder-bench /usr/local/bin/ladder-bench

# A run writes the reference clip, the encodes and the VMAF logs into its work
# dir, and those files land on the host through a bind mount. Running as root
# would leave them root-owned in someone's project directory, so the image has
# a real user — and uid 1000 is the one a single-user Linux host hands out
# first, which makes the common bind mount writable with no flags at all.
RUN adduser -D -u 1000 ladder \
 && mkdir -p /work \
 && chown ladder:ladder /work
USER ladder
WORKDIR /work

# Subcommands are the arguments, so `docker run <image> doctor` works and reads
# like the CLI it is. Bare `docker run <image>` prints the usage and exits 0,
# rather than exiting 64 on a container nobody asked a question of.
ENTRYPOINT ["ladder-bench"]
CMD ["help"]
