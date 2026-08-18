---
title: Docker
nav_order: 6
nav_blurb: An image that already has libvmaf
description: >-
  Running ladder-bench from a container image that ships an ffmpeg built with
  libvmaf — the flags that matter, what is inside the image, how to build it
  yourself, and what containerising changes about the measurement.
---

# Docker

```bash
docker run --rm -v "$PWD:/work" ghcr.io/allan-nava/ladder-bench doctor
```

## Why an image exists at all

ladder-bench needs an **ffmpeg built with libvmaf**, and libvmaf is a
compile-time option that no distribution enables. All four of these install a
perfectly healthy ffmpeg that cannot measure anything:

| Base | ffmpeg | libvmaf |
|---|---|---|
| Debian 12 `bookworm` | 5.1.9 | no |
| Debian 13 `trixie` | 7.1.5 | no |
| Ubuntu 24.04 | 6.1.1 | no |
| Alpine 3.21 | 6.1.2 | no |

Getting one that does means building ffmpeg, hunting a static build, or using
this image. It exists so that "install ffmpeg" stops being the hard part.

## The three flags that matter

```bash
docker run --rm -v "$PWD:/work" --user "$(id -u):$(id -g)" \
  ghcr.io/allan-nava/ladder-bench run --output markdown
```

| Flag | Why |
|---|---|
| `-v "$PWD:/work"` | `/work` is the working directory inside the container, so a config named `ladder-bench.yml` in the current directory is found with no `--config` at all. Without a mount the container has no source file, and the report it writes disappears with it. |
| `--user "$(id -u):$(id -g)"` | A run writes the reference clip, the encodes and the VMAF logs into `work_dir`, which is your directory through the bind mount. The image already runs as uid 1000, so on a single-user Linux host this flag changes nothing — pass it when your uid is not 1000, and the files come out yours instead of someone else's. |
| `--rm` | The container is a command, not a service. Everything worth keeping is in the mounted directory. |

Subcommands are the arguments, so the container reads like the CLI:
`… ladder-bench init`, `… ladder-bench plan`, `… ladder-bench run --output json`.
Running it with no arguments prints the usage.

A shell function makes it disappear entirely:

```bash
ladder-bench() {
  docker run --rm -v "$PWD:/work" --user "$(id -u):$(id -g)" \
    ghcr.io/allan-nava/ladder-bench "$@"
}
```

The image also carries the ffmpeg, which is occasionally what you actually
want — for cutting a test clip, for instance:

```bash
docker run --rm -v "$PWD:/work" --user "$(id -u):$(id -g)" \
  --entrypoint ffmpeg ghcr.io/allan-nava/ladder-bench \
  -f lavfi -i "testsrc2=size=1920x1080:rate=25" -t 8 \
  -c:v libx264 -crf 18 -pix_fmt yuv420p source.mp4
```

## What is inside

- **ffmpeg 7.1 and ffprobe**, static builds with **libvmaf**, x264, x265,
  SVT-AV1 and VP9, taken from
  [`mwader/static-ffmpeg`](https://hub.docker.com/r/mwader/static-ffmpeg) and
  pinned by **manifest list digest** in the [`Dockerfile`](https://github.com/Allan-Nava/ladder-bench/blob/main/Dockerfile).
  A digest rather than a tag because an ffmpeg upgrade can move the numbers, and
  that belongs in a commit message rather than in whatever the registry served
  that morning. Pinning the *list* digest keeps the image multi-arch.
- **ladder-bench**, built static (`CGO_ENABLED=0`) with the release tag compiled
  into it, so `ladder-bench version` and every report header say which version
  produced them.
- **Alpine** underneath, with a real non-root user. Alpine rather than `scratch`
  because a shell is what turns "the container did something odd" into a session
  you can look around in: `docker run --rm -it --entrypoint sh …`.

Around 220 MB, for `linux/amd64` and `linux/arm64`.

Tags follow the releases: `0.4.0` for an exact version, `0.4` for the latest
patch of a minor, `latest` for the newest release. Pin the exact version in
anything whose numbers you intend to compare over time.

## Building it yourself

```bash
docker build -t ladder-bench --build-arg VERSION="$(git describe --tags)" .
```

`VERSION` is what the binary reports; it defaults to `docker` when you leave it
out, which is honest about a build that came from no particular tag. The build
context is a whitelist — `go.mod`, `go.sum`, `cmd/` and `internal/` and nothing
else — so the image never picks up a stray work dir or a local config.

## What containerising changes about the measurement

**Quality per bit does not change.** The encoders and libvmaf are the same code
whether they run in a container or not, and the same grid produces the same VMAF.

**Timings do.** Every report records how long each point took to encode, and
that number is about the CPU the container was given. Two things make it lie:

- **A CPU limit.** `--cpus` or a Kubernetes limit changes encode times without
  changing quality. Fine for the ladder, misleading if you are reading the
  timings.
- **Emulation.** The amd64 image runs on an arm64 host (and the reverse) through
  QEMU. It works, and it is slow enough that the timings mean nothing at all.
  Docker prints a `platform … does not match` warning when this happens — it is
  worth reading. Use the image matching your host, which is what a plain
  `docker pull` gives you.

**Disk lands on the host.** `work_dir` is inside the mount, so the encodes and
the reference clip are counted against your filesystem, not the container's.
`ladder-bench plan` prints the estimate before anything runs.

## In CI

The image removes the awkward part of a CI setup — no static ffmpeg to fetch, no
container image to go hunting for. See [In CI](ci.md) for a scheduled workflow
that posts the report to a job summary.

Call it with `docker run` per command rather than as a GitHub Actions job
`container:`. A job container has to run GitHub's own Node.js for every
JavaScript action, `actions/checkout` included, and that is a glibc build which
does not start on this image's musl base.
