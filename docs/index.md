---
title: Overview
nav_order: 1
nav_blurb: What it is and the first run
description: >-
  ladder-bench measures an ABR encoding ladder instead of inheriting it — it
  encodes a reference clip across a grid of resolutions and bitrates, scores
  every point with VMAF, and reports saturation, the efficient frontier, a
  recommended ladder and the BD-rate between encoders.
---

# ladder-bench

**Measure your ABR encoding ladder instead of inheriting it.**

Most ladders are copied from a blog post written about someone else's content
and then never questioned. ladder-bench replaces that inheritance with a
measurement: it cuts a reference clip from your own source, encodes it across a
grid of (resolution × bitrate × encoder) points, scores every point with
**VMAF**, and reports four things.

1. **Where each resolution stops paying** — the bitrate past which more bits buy
   no visible quality. See [saturation](method.md#4-the-analysis).
2. **Which resolution wins at each bitrate** — the efficient frontier, the
   per-title idea in one line.
3. **What your ladder should be** — rungs spaced by perceived quality, plus what
   the ladder you ship today costs against what the same quality costs on the
   frontier.
4. **What another encoder would cost you** — the [BD-rate](method.md#4-the-analysis)
   against the codec you ship today, measured on your content instead of quoted
   from a press release.

One static Go binary. No agent, no server, no account: it drives `ffmpeg` and
`ffprobe` as subprocesses, and every command it runs can be
[printed before it runs](cli.md#plan).

## Install

```bash
go install github.com/Allan-Nava/ladder-bench/cmd/ladder-bench@latest
```

Pre-built binaries are attached to each
[release](https://github.com/Allan-Nava/ladder-bench/releases).

ladder-bench needs an **ffmpeg built with libvmaf** (`--enable-libvmaf`;
Homebrew's ffmpeg has it). libvmaf is a build-time option, so a perfectly
healthy ffmpeg may simply not have it — `ladder-bench doctor` says whether yours
does, before an hour of encodes finds out the hard way.

## Sixty seconds

```bash
ladder-bench init > ladder-bench.yml   # a commented starting config
$EDITOR ladder-bench.yml               # point `input:` at real content
ladder-bench doctor                    # ffmpeg, libvmaf, codecs, input, disk
ladder-bench plan                      # the exact commands, without running them
ladder-bench run                       # encode, measure, report
```

No real content at hand? A synthetic clip is enough to see the shape of the
output, though not to make a decision on:

```bash
ffmpeg -f lavfi -i "testsrc2=size=1920x1080:rate=25" -t 8 \
       -c:v libx264 -crf 18 -pix_fmt yuv420p source.mp4
```

A run is **resumable**: finished points are reused, so an interrupted grid picks
up where it stopped, and re-rendering the same measurements in another format
costs nothing. See [`--force` and the work dir](cli.md#run).

## What a report looks like

```
  saturation
    1080p  flattens at 3037k (VMAF 93.1) — the top 51% of this grid's bitrate buys nothing
    720p   flattens at 1461k (VMAF 84.8) — the top 51% of this grid's bitrate buys nothing
    480p   still climbing at the top of the grid — extend it upward

  vs current ladder — same quality, 2 of 2 rungs comparable
    9000k → 7885k  (-12.4%)
    720p 3000k   → 1080p 1885k  same quality (VMAF 88.38)  -37.2%
    1080p 6000k  → 1080p 6000k  same quality (VMAF 96.70)  -0.0%
```

That last block is the one that pays for the run: *this 720p rung delivers VMAF
88.4, and 1080p delivers the same for 37% fewer bits.* The whole output, block
by block, is on [The report](output.md).

## The one rule the tool follows

**It never invents a number.** A rung outside the measured grid is reported as
outside the grid; a curve that never flattened is reported as still climbing; a
pair of encoders whose quality ranges barely overlap gets no BD-rate at all.
Extrapolating a rate-quality curve would produce exactly the confident estimate
this tool exists to replace, so nothing here interpolates past its data.

[What it does *not* measure](method.md#what-this-does-not-measure) is on the
method page, and worth reading before quoting a percentage at anyone.

## Where to go next

- **[Configuration](configuration.md)** — every key of `ladder-bench.yml`.
- **[Method](method.md)** — what happens to a file and why. Read this before
  acting on a report.
- **[The report](output.md)** — how to read each block, and the JSON schema.
- **[CLI reference](cli.md)** — every command, flag and exit code.
- **[In CI](ci.md)** — scheduled runs, caching, job summaries.
