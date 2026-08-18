---
title: The report
nav_order: 4
nav_blurb: Reading each block, and the JSON schema
description: >-
  How to read a ladder-bench report block by block — the measurements table,
  saturation, the efficient frontier, the recommended ladder, the comparison
  against your current ladder and the BD-rate — plus every field of the JSON
  output.
---

# The report

The same run renders three ways, chosen with `--output`. They are projections of
one structure: nothing is computed in a renderer that the analysis did not
already decide, so the three never disagree.

| Format | For | Notes |
|---|---|---|
| `text` *(default)* | A terminal | Aligned columns, the phrasing quoted throughout this page. |
| `markdown` | A pull request, a wiki page, a CI job summary | The same blocks as tables. |
| `json` | A plot, a diff against last season's run, an archive | Every measurement, including the ones the text report summarises. |

The report goes to **stdout** and progress to **stderr**, so redirecting the
report never swallows the progress and vice versa. `--out FILE` writes the
report to a file instead; a report truncated by a full disk fails the command
rather than exiting `0` with half a table.

Re-rendering costs nothing. Every point is already on disk, so a second `run`
with a different `--output` only re-reads the measurements.

## The header

```
ladder-bench 0.2.0 — 2026-08-17T22:46:51Z
source     source.mp4  1920x1080  25.00 fps  h264  6.0s
reference  .lb-smoke/reference_full_6s.mkv  1920x1080  6.0s  (reused)
measured   12 points across 2 encoder(s)
```

`source` is the file the clip was cut from; `reference` is the lossless clip
every encode and every comparison actually used. `(reused)` means the clip was
already on disk from an earlier run — its name carries the cut, so this is a
reuse of the same frames, not of a stale file.

## Measurements

```
  RES   TARGET  ACTUAL  VMAF   HMEAN  MIN    GAIN/+10%  PSNR-Y  SSIM
  720p  600k    651k    80.29  80.09  72.94  —          35.88   0.9910
  720p  1500k   1544k   93.12  93.09  90.14  0.94       40.49   0.9973
  720p  3000k   3108k   97.18  97.17  95.49  0.40       46.26   0.9992
```

| Column | What it is |
|---|---|
| `RES` | The rung's height. Width follows the source aspect ratio, rounded to an even number. |
| `TARGET` | The bitrate the grid asked for. |
| `ACTUAL` | The bitrate the file **is** — its size over the clip duration. Rate control never lands exactly on target, and every number downstream uses this one. |
| `VMAF` | The pooled mean over the clip, measured at the reference resolution. This is the column every recommendation is computed from. |
| `HMEAN` | The harmonic mean of the same frames, which weighs the worst ones more heavily. Well below `VMAF` means a few seconds fell apart and the average absorbed it; agreeing with it means the rung was consistently that good. |
| `MIN` | The worst single frame. A rung whose mean is 93 and whose minimum is 70 is not a rung that looks like 93. |
| `GAIN/+10%` | VMAF points bought by this step, per +10% bitrate. Relative on purpose: +500 kbps means something entirely different at 800 kbps and at 6000 kbps. The first row of each resolution has no previous step, hence `—`. |
| `PSNR-Y` | Peak signal-to-noise ratio in dB, Y plane. Present only with [`vmaf.metrics`](configuration.md#vmaf). |
| `SSIM` | Structural similarity, 0–1. Present only with `vmaf.metrics`. |

`PSNR-Y` and `SSIM` **have no column at all** unless the run asked for them: a
column of dashes would say "we looked and found nothing" instead of "we did not
look". Within a column, a `—` is a point whose log predates the request — those
get re-measured on the next run, so it should not persist.

The extra metrics are reported, never acted on. Everything below this table —
saturation, the frontier, the ladder, the BD-rate — comes from `VMAF` alone.

## Saturation

The knee: the last point whose bits still buy quality. Four things this block can
say, and each is a different instruction.

```
  saturation
    1080p  flattens at 3056k (VMAF 92.2) — the top 50% of this grid's bitrate buys nothing
    720p   already flat at the cheapest point measured (826k) — extend the grid downward
    480p   still climbing at the top of the grid — extend it upward
    360p   not enough points to tell
```

- **flattens at X** — the answer. Past that bitrate every measured step gains
  less than `knee_gain` VMAF per +10%, and the percentage is what sits between
  the knee and the top of *this rung's* grid.
- **already flat at the cheapest point measured** — the grid started above the
  interesting range, so the knee could be anywhere below it. Add cheaper points.
- **still climbing at the top of the grid** — the curve never flattened inside
  the grid. There is no knee to report, and the tool will not guess one.
- **not enough points to tell** — fewer than two usable points at that
  resolution.

## Efficient frontier

```
  efficient frontier
    720p 786k (VMAF 79.7) → 1080p 1528k (VMAF 85.5) → 1080p 3056k (VMAF 92.2) → 1080p 6098k (VMAF 96.2)
```

Every resolution's points pooled, keeping only the ones no other point beats on
both bitrate and quality at once. Read left to right it answers *at this
bitrate, which resolution looks best* — which is exactly what a ladder encodes,
and why a well-encoded 720p rung can outrank a starved 1080p one. Points that
cost more and score less never appear: as advice they would read "pay more, see
less".

## Recommended ladder

```
  recommended ladder (steps of 6.0 VMAF, target 93.0)
    1080p  6098k  VMAF 96.17
    1080p  1528k  VMAF 85.54
    total 7625k
```

Rungs come **off the frontier**, so every one is a point that was really
measured — no interpolated bitrate ever reaches the ladder. They are spaced by
`ladder_step` VMAF (~6, roughly one just-noticeable difference) rather than by
the usual bitrate halving, which piles up indistinguishable rungs at the top and
leaves a cliff at the bottom. The top rung is the *cheapest* point reaching
`target_vmaf`, not the best point measured.

When nothing reached the target, the block says so:

```
    ! no measured point reached VMAF 93.0 — the top rung is the best the grid could do
```

`total` is **informational only**. It aims at `target_vmaf`, not at whatever your
current ladder happens to deliver, and two ladders with different rung counts
cannot be compared by their sums.

## Versus your current ladder

```
  vs current ladder — same quality, 2 of 2 rungs comparable
    9000k → 8064k  (-10.4%)
    1080p 6000k  → 1080p 6000k  same quality (VMAF 96.04)  -0.0%
    720p 3000k   → 1080p 2064k  same quality (VMAF 87.88)  -31.2%
```

For each rung of `current_ladder`: the quality it delivers today, interpolated
within its own resolution's measured curve, and the cheapest place on the
frontier that reaches **the same** quality. This is the like-for-like number —
same quality, fewer bits — and the one worth quoting.

The totals sum **only the comparable rungs**, which is what `2 of 2 rungs
comparable` is counting. Putting an unmeasured rung on one side of a total and
nothing on the other would invent a saving out of a missing measurement.

A rung can decline to be compared, in which case it appears with a reason
instead of a number:

- `outside the measured grid at this resolution` — the grid never covered that
  rung. Rungs within 5% of the ends of the measured range are snapped to the end
  first, because rate control never lands on target; further out is refused
  rather than extrapolated.
- `no point on the frontier reaches this quality` — the rung delivers more than
  anything measured, so there is nothing to price it against.

## BD-rate

Present only when the run measured **two or more encoders**.

```
bd-rate vs x264-fast — bitrate for the same measured quality, negative is cheaper

  x265-fast
    frontier  -31.9%  over VMAF 85.0–96.1  cubic fit
    1080p     -24.5%  over VMAF 90.0–96.1  piecewise linear
    720p      -46.0%  over VMAF 85.0–87.9  piecewise linear
```

One percentage per scope: how much bitrate the challenger needs for the same
measured quality. The **anchor** is the first encoder listed in the config — the
one you ship today — so negative always means the challenger is cheaper.

- `frontier` compares the two efficient frontiers: the ladder-level answer, where
  each encoder is free to pick its best resolution at each bitrate.
- The per-resolution rows compare like against like at one height.
- `over VMAF a–b` is the quality range **both** encoders reached. The average is
  taken there and nowhere else; outside the overlap there is nothing to compare
  against.
- `cubic fit` or `piecewise linear` says how each curve was integrated. Four or
  more points get the classic least-squares cubic; two or three are interpolated
  between the measurements, because a cubic through three points is not a fit but
  a curve drawn through noise. A pair is labelled with the coarser of its two
  curves.

A row without a number carries the reason:

- `the two curves share less than 1.0 VMAF of quality — nothing to average over`
  — a percentage taken over a sliver looks like a verdict and is really the noise
  of a couple of frames.
- `not enough measured points on both curves` — fewer than two usable points on
  one side.
- `the anchor curve could not be integrated (two points at the same quality?)` —
  and the same for the test curve. Two different bitrates scoring identically is
  not a function of quality, so there is no single bitrate to read off.

The arithmetic behind all of it is on [Method](method.md#4-the-analysis).

## Markdown

`--output markdown` renders the same blocks as tables under `##` headings, ready
to paste into a pull request or append to `$GITHUB_STEP_SUMMARY`. See
[In CI](ci.md#a-scheduled-report).

## JSON

`--output json` is the archive format: every measurement, not only the ones the
text report summarises. It is what a plot reads, and what a future `compare`
between two runs will diff.

### Top level

| Field | Type | What it is |
|---|---|---|
| `tool` | string | Always `ladder-bench`. |
| `version` | string | The binary's version, or `dev` for a build from source. |
| `generated` | string | RFC 3339, UTC. |
| `input` | string | The source file. |
| `reference` | object | The clip every measurement used. |
| `options` | object | The analysis thresholds this report was computed with. |
| `results` | array | One entry per grid point — the raw measurements. |
| `analysis` | array | One entry per encoder — everything derived. |
| `bd_rates` | array | Cross-encoder comparisons. **Absent** with a single encoder: nothing was compared, which is not the same as finding no difference. |

### `reference`

| Field | Type | What it is |
|---|---|---|
| `path` | string | The lossless clip on disk. Its name carries the cut. |
| `media` | object | Geometry of the clip: `codec`, `width`, `height`, `frame_rate`, `duration_s`. |
| `source` | object | The same shape, for the file it was cut from. |
| `reused` | bool | The clip was already on disk. |

### `options`

| Field | Type | What it is |
|---|---|---|
| `knee_gain` | number | VMAF per +10% bitrate below which a rung counts as saturated. |
| `target_vmaf` | number | Quality the top recommended rung aims at. |
| `ladder_step` | number | VMAF distance between recommended rungs. |
| `current_ladder` | array | `{height, kbps}` per configured rung, when one was given. |

### `results[]`

One per grid point. This is the only place the per-point cost and timings live.

| Field | Type | What it is |
|---|---|---|
| `encoder`, `codec`, `preset`, `extra_args` | | The encoder configuration this point used. |
| `height`, `target_kbps` | number | The grid coordinates. |
| `out`, `vmaf_log` | string | The encode and the libvmaf log on disk. The encode is deleted after the run unless `keep_encodes` is set; the log stays, because it is the measurement. |
| `vmaf` | object | `mean`, `min`, `harmonic_mean`, `frames` as libvmaf pooled them, plus `psnr_y` and `ssim` when the run asked for them. Both keys are **absent** rather than zero when it did not: a PSNR of 0 dB would be a catastrophe, and absent is not catastrophic. |
| `bytes` | number | The encode's real size. |
| `actual_kbps` | number | `bytes` over the clip duration — the bitrate every downstream number uses. |
| `encode_ns`, `measure_ns` | number | Wall-clock nanoseconds for the encode and the VMAF pass. `0` for a reused point. |
| `reused` | bool | The point came off disk instead of being re-measured. |

### `analysis[]`

One per encoder. A hull mixing two codecs would recommend a ladder no single
encoder can produce, so the analysis never pools them.

| Field | Type | What it is |
|---|---|---|
| `encoder` | string | Which encoder this analysis is of. |
| `curves` | array | One per resolution, tallest first. |
| `hull` | array | The efficient frontier, cheapest first. |
| `ladder` | array | The recommended rungs, best first. |
| `savings` | array | One per configured `current_ladder` rung. |
| `ladder_total_kbps` | number | Sum of the recommended rungs — informational, see above. |
| `current_total_kbps`, `efficient_total_kbps` | number | The like-for-like totals over the comparable rungs only. |
| `compared_rungs` | number | How many rungs of the current ladder the grid could measure. |
| `total_saved_pct` | number | Change between those two totals. |
| `target_reached` | bool | `false` when no measured point reached `target_vmaf`: the top rung is then the best the grid could do, not the quality asked for. |

**A point** (used in `curves[].points`, `hull` and `ladder`):
`encoder`, `height`, `target_kbps`, `kbps` (the real one), `vmaf`, `vmaf_min`,
`vmaf_harmonic_mean`, and `psnr_y` / `ssim` when they were measured. Only `kbps`
and `vmaf` take part in the arithmetic; the rest is carried so a rung can be
judged on more than its mean.

**A curve** (`curves[]`): `height`, `points`, and

| Field | Type | What it is |
|---|---|---|
| `knee` | point | The last point whose bits still buy quality. Absent when there is none. |
| `flat_from_start` | bool | Even the cheapest measured point was already saturated — extend the grid downward. |
| `still_climbing` | bool | The curve never flattened inside the grid — extend it upward. |
| `wasted_pct` | number | Share of this rung's top measured bitrate that sits above the knee. |

**A saving** (`savings[]`): `current` (`{height, kbps}`), `current_vmaf`,
`efficient_kbps`, `efficient_height`, `saved_pct`, and `note` when the rung could
not be compared. When `note` is set the numbers are zero because nothing was
computed — not because there was nothing to save.

### `bd_rates[]`

| Field | Type | What it is |
|---|---|---|
| `anchor`, `test` | string | Which encoder is which. Negative rates mean `test` is cheaper. |
| `frontier` | object | The comparison between the two efficient frontiers. |
| `by_height` | array | The same, per resolution, tallest first. |

**A BD figure** (`frontier` and `by_height[]`):

| Field | Type | What it is |
|---|---|---|
| `height` | number | The resolution, or absent on the frontier figure. |
| `low_vmaf`, `high_vmaf` | number | The quality interval both encoders reached — the only range the average covers. |
| `rate_pct` | number | Bitrate difference at equal quality, test against anchor. `-18.4` means 18.4% fewer bits for the same VMAF. |
| `method` | string | `cubic fit` or `piecewise linear`. |
| `anchor_points`, `test_points` | number | How many measurements backed each curve. |
| `note` | string | Why no figure was produced. When it is set, `rate_pct` is `0` because nothing was computed. |

### Reading it from a shell

```bash
ladder-bench run --output json --out ladder.json

# every measured point of one encoder, as bitrate/VMAF pairs
jq -r '.analysis[] | select(.encoder=="x264-fast")
       | .curves[] | .height as $h | .points[]
       | "\($h)p \(.kbps|round)k \(.vmaf)"' ladder.json

# every rung where the harmonic mean fell more than 2 points below the mean
jq -r '.results[] | select(.vmaf.harmonic_mean < .vmaf.mean - 2)
       | "\(.height)p \(.target_kbps)k mean \(.vmaf.mean) harmonic \(.vmaf.harmonic_mean)"' ladder.json

# the headline BD-rate
jq -r '.bd_rates[]? | "\(.test) vs \(.anchor): \(.frontier.rate_pct // "n/a")%"' ladder.json
```
