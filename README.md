<p align="center">
  <img src="docs/assets/logo.svg" alt="" width="96" height="96">
</p>

<h1 align="center">ladder-bench</h1>

<p align="center"><strong>Measure your ABR encoding ladder instead of inheriting it.</strong></p>

<p align="center">
  <a href="https://github.com/Allan-Nava/ladder-bench/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Allan-Nava/ladder-bench/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-10b981"></a>
  <img alt="Go" src="https://img.shields.io/github/go-mod/go-version/Allan-Nava/ladder-bench?color=10b981">
  <a href="https://allan-nava.github.io/ladder-bench/"><img alt="Documentation" src="https://img.shields.io/badge/docs-allan--nava.github.io-10b981"></a>
</p>

---

Most streaming ladders are inherited, not measured. The bitrates come from a
preset someone published years ago, and nobody re-checks them against the
content actually being shipped — even though a static talk show and a football
match have completely different appetites for bits.

**ladder-bench** replaces that guess with a measurement. It cuts a reference
clip out of your source, encodes it across a grid of (resolution, bitrate)
points, scores every point with **VMAF**, and reports four things:

1. **Where each resolution stops paying** — the bitrate past which more bits
   buy no visible quality.
2. **Which resolution wins at each bitrate** — the efficient frontier, the
   per-title idea in one line.
3. **What your ladder should be** — rungs spaced by perceived quality, and what
   the ladder you ship today costs versus what the same quality costs on the
   frontier.
4. **What another encoder would cost you** — the **BD-rate** against the codec
   you ship today, measured on your content instead of quoted from a press
   release.

One static Go binary. No agent, no server, no account. It drives `ffmpeg` and
`ffprobe`, and every command it runs can be printed before it runs.

## Install

```bash
go install github.com/Allan-Nava/ladder-bench/cmd/ladder-bench@latest
```

Requires an **ffmpeg built with libvmaf** (`--enable-libvmaf`; Homebrew's
ffmpeg has it). `ladder-bench doctor` tells you if yours does.

## Use

```bash
ladder-bench init > ladder-bench.yml   # a commented starting config
$EDITOR ladder-bench.yml               # point `input:` at real content
ladder-bench doctor                    # ffmpeg, libvmaf, codecs, input, disk
ladder-bench plan                      # the exact commands, without running them
ladder-bench run                       # encode, measure, report
```

`--output markdown` produces a report for a PR or a wiki page; `--output json`
gives you every measurement for a plot or a diff against last season's run.

A run is resumable: finished points are reused, so an interrupted grid picks up
where it stopped (`--force` re-encodes everything).

## What the report looks like

```
encoder x264-fast (libx264, preset veryfast)
  RES    TARGET  ACTUAL  VMAF   HMEAN  MIN    GAIN/+10%
  1080p  1500k   1489k   86.74  85.90  79.90  —
  1080p  3000k   3037k   93.14  92.71  90.25  0.62
  1080p  6000k   6136k   96.87  96.62  95.47  0.37
  720p   800k    736k    79.63  78.02  71.30  —
  720p   1500k   1461k   84.79  83.74  77.10  0.53
  720p   3000k   2974k   88.38  87.55  83.72  0.35

  saturation
    1080p  flattens at 3037k (VMAF 93.1) — the top 51% of this grid's bitrate buys nothing
    720p   flattens at 1461k (VMAF 84.8) — the top 51% of this grid's bitrate buys nothing
    480p   still climbing at the top of the grid — extend it upward

  efficient frontier
    480p 333k (VMAF 66.1) → 720p 736k (VMAF 79.6) → 1080p 1489k (VMAF 86.7) → 1080p 3037k (VMAF 93.1)

  recommended ladder (steps of 6.0 VMAF, target 93.0)
    1080p  3037k  VMAF 93.14
    1080p  1489k  VMAF 86.74
    720p   736k   VMAF 79.63
    480p   333k   VMAF 66.08

  vs current ladder — same quality, 2 of 2 rungs comparable
    9000k → 7885k  (-12.4%)
    720p 3000k   → 1080p 1885k  same quality (VMAF 88.38)  -37.2%
    1080p 6000k  → 1080p 6000k  same quality (VMAF 96.70)  -0.0%
```

The last block is the one that pays for the run: *this 720p rung delivers VMAF
88.4, and 1080p delivers the same for 37% fewer bits.*

Put a second encoder in the config and the run ends with the codec question
answered on your own content:

```
bd-rate vs x264-fast — bitrate for the same measured quality, negative is cheaper

  x265-fast
    frontier  -31.9%  over VMAF 85.0–96.1  cubic fit
    1080p     -24.5%  over VMAF 90.0–96.1  piecewise linear
    720p      -46.0%  over VMAF 85.0–87.9  piecewise linear
```

The anchor is the first encoder in the config — the one you ship today. The
average is taken only over the quality range **both** encoders reached, never
over the union of the two, and a pair of curves that barely overlap is declined
rather than stretched to meet. Curves with four or more points get the classic
least-squares cubic fit; shorter ones are interpolated between the measurements
and the report says so, because a cubic through three points is not a fit.

## How it measures

The details below are the difference between numbers you can act on and numbers
that merely look precise.

- **The reference is cut once, losslessly.** A stream copy can only cut on a
  keyframe, so every encode would start on a different frame. The clip is
  normalised to 8-bit yuv420p, which is what the VMAF models are trained on.
- **The distorted encode is scaled back up to the reference resolution before
  scoring.** VMAF models a viewer on a full-size screen, so a 360p rung has to
  be judged as the upscaled picture the viewer actually sees. Scoring at native
  resolution would make every low rung look excellent and the cross-resolution
  comparison meaningless.
- **Rate control is capped** (`maxrate`/`bufsize`), not plain average bitrate.
  A rung that overshoots its declared bandwidth is a rung the player abandons,
  so measuring an uncapped encode would score quality nobody can deliver.
- **The GOP is fixed** to your segment duration. Longer GOPs report an
  efficiency you cannot ship in an ABR delivery.
- **No extrapolation.** A rung outside the measured grid is reported as such
  rather than guessed — replacing guesses is the point of the tool.
- **The ladder rungs are measured points**, never interpolated bitrates, and
  they are spaced by ~6 VMAF (roughly one just-noticeable difference) instead
  of by the usual bitrate halving.
- **More than one number per point.** Every report carries the VMAF **harmonic
  mean** next to the mean — when it sits well below, a few seconds of the clip
  fell apart and the average absorbed it. `vmaf.metrics: [psnr, ssim]` adds
  PSNR-Y and SSIM in the same pass, for a fraction of its cost. They are
  reported, never acted on: VMAF alone drives every recommendation, because
  averaging two metrics into one score only hides which one you were trusting.

## Cost

A grid point costs one encode plus one VMAF pass over the clip. A 12-point grid
on a 30-second clip is minutes, not hours, with `x264 -preset slow`; AV1 is
another matter. `vmaf.n_subsample: 5` makes the measuring pass several times
faster and is usually good enough for a curve.

## Documentation

**<https://allan-nava.github.io/ladder-bench/>** — the same Markdown that lives
in [`docs/`](docs/), published as a site.

- [Overview](https://allan-nava.github.io/ladder-bench/) — what it is and the
  first run
- [Configuration](https://allan-nava.github.io/ladder-bench/configuration/) —
  every key of `ladder-bench.yml`
- [Method](https://allan-nava.github.io/ladder-bench/method/) — what happens to
  a file and why, and what the numbers do *not* cover
- [The report](https://allan-nava.github.io/ladder-bench/output/) — every block
  it prints, and the full JSON schema
- [CLI reference](https://allan-nava.github.io/ladder-bench/cli/) — every
  command, flag and exit code
- [In CI](https://allan-nava.github.io/ladder-bench/ci/) — scheduled runs,
  caching, job summaries
- [`BACKLOG.md`](BACKLOG.md) — what is planned, with stable ids

## License

MIT © Allan Nava
