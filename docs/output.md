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
ladder-bench 0.5.0 — 2026-08-17T22:46:51Z
source     source.mp4  1920x1080  25.00 fps  h264  6.0s
reference  .lb-smoke/reference_full_6s.mkv  1920x1080  6.0s  (reused)
measured   12 points across 2 encoder(s)
ffmpeg     ffmpeg version 7.1 Copyright (c) 2000-2024 the FFmpeg developers
libvmaf    3.0.0
config     8975adcae3f1
```

`source` is the file the clip was cut from; each `reference` line is a lossless
clip encodes were made from and compared against — one per entry in
[`clips:`](configuration.md#clips), and the header says how many. `(reused)` means the clip was
already on disk from an earlier run — its name carries the cut, so this is a
reuse of the same frames, not of a stale file.

The last three lines are what makes the report re-checkable rather than merely
believable: the ffmpeg that ran it, every libvmaf that wrote one of its logs, and
a short fingerprint of the resolved config. Two reports with the same fingerprint
measured the same experiment; two with different ones did not, whatever their
tables look like. See [Method](method.md#5-what-the-report-records-about-itself)
for exactly what is and is not hashed.

When more than one libvmaf wrote the logs, the header says so:

```
libvmaf    2.3.1, 3.0.0
           ! these points were not all measured by the same libvmaf — re-run with --force to compare them
```

That happens to a work dir that survived an ffmpeg upgrade. The numbers on either
side of it are fine; putting them on one curve is not.

## Measurements

```
  RES   TARGET  ACTUAL  VMAF   HMEAN  P5     P1     MIN    GAIN/+10%  PSNR-Y
  720p  800k    711k    70.15  56.56  16.78  16.35  15.36  —          33.04
  720p  2500k   2030k   86.12  68.44  18.12  17.00  16.95  0.86       38.39
```

That is a real run on a clip with one broken second in the middle of it, and it
is the argument for the whole table: **VMAF says 70, the 1st percentile says 16**.
A single mean would have called the cheap rung acceptable.

| Column | What it is |
|---|---|
| `RES` | The rung's height. Width follows the source aspect ratio, rounded to an even number. |
| `TARGET` | The bitrate the grid asked for. |
| `ACTUAL` | The bitrate the file **is** — its size over the clip duration. Rate control never lands exactly on target, and every number downstream uses this one. |
| `VMAF` | The pooled mean over the clip, measured at the reference resolution. This is the column every recommendation is computed from. |
| `HMEAN` | The harmonic mean of the same frames, which weighs the worst ones more heavily. Well below `VMAF` means a few seconds fell apart and the average absorbed it; agreeing with it means the rung was consistently that good. |
| `P5`, `P1` | The 5th and 1st percentile of the per-frame scores — the worst moments, by nearest rank, so each is a score some frame really received. Present only when the log has a per-frame section. |
| `MIN` | The worst single frame, i.e. P0. Useful next to `P1`: when they are far apart, one frame was an outlier; when they agree, the tail is genuinely that bad. |
| `GAIN/+10%` | VMAF points bought by this step, per +10% bitrate. Relative on purpose: +500 kbps means something entirely different at 800 kbps and at 6000 kbps. The first row of each resolution has no previous step, hence `—`. |
| `PSNR-Y` | Peak signal-to-noise ratio in dB, Y plane. Present only with [`vmaf.metrics`](configuration.md#vmaf). |
| `SSIM` | Structural similarity, 0–1. Present only with `vmaf.metrics`. |
| `SPREAD` | The VMAF distance between the best and worst clip at this point. Present only with more than one [clip](configuration.md#clips). |

`P5` and `P1` come out of the per-frame section every real libvmaf log already
carries, so no point is ever re-measured to obtain them — and `n_subsample`
coarsens them, because they cover the frames that were scored rather than all of
them.

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

## Across clips

Present only when the run measured more than one [clip](configuration.md#clips).

```
  across 3 clips
    widest disagreement 81.95 VMAF at 720p 377k
    ! that is wider than the 6.0 VMAF between rungs — the clips disagree by more than a whole rung,
      so measure more of the content before trusting the ladder to a single number
```

Every row of the table above it is an **average across the clips**, and this block
is what that average cost. A spread of two or three VMAF means the cuts broadly
agreed and the ladder is sound. A spread wider than `ladder_step` means two cuts
of your own source disagree about that rung by more than a whole rung: the
recommended ladder is then an average of two different answers, and the honest
next step is to measure more content rather than to trust the number harder.

That example is a real run on a source deliberately built from a flat colour, a
detail pattern and pure noise — the extreme case, and the shape to look for.

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
| `references` | array | One entry per clip: `path`, `clip` (the name it is known by, absent for a single-clip run), `media`, `source`, `reused`. |
| `options` | object | The analysis thresholds this report was computed with. |
| `results` | array | One entry per grid point — the raw measurements. |
| `analysis` | array | One entry per encoder — everything derived. |
| `bd_rates` | array | Cross-encoder comparisons. **Absent** with a single encoder: nothing was compared, which is not the same as finding no difference. |
| `environment` | object | What measured this: `ffmpeg` (the version line, verbatim), `libvmaf` (every version that wrote one of these logs), `config_sha256` (the fingerprint of the resolved config, in full — the text report shows the first twelve characters). |

### `references[]`

| Field | Type | What it is |
|---|---|---|
| `path` | string | The lossless clip on disk. Its name carries the cut. |
| `clip` | string | How this cut is named in `results[].clip` and in the file names. Absent for a single-clip run. |
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
| `clip` | string | Which cut this point was measured on. Absent for a single-clip run. |
| `out`, `vmaf_log` | string | The encode and the libvmaf log on disk. The encode is deleted after the run unless `keep_encodes` is set; the log stays, because it is the measurement. |
| `vmaf` | object | `mean`, `min`, `harmonic_mean`, `frames` as libvmaf pooled them; `vmaf_p1` and `vmaf_p5` from the per-frame section; `libvmaf_version` — recorded per point, so a grid resumed across an ffmpeg upgrade shows which points came from which build; plus `psnr_y` and `ssim` when the run asked for them. The optional keys are **absent** rather than zero when they were not measured: a PSNR of 0 dB would be a catastrophe, and absent is not catastrophic. |
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
`vmaf_harmonic_mean`, `vmaf_p1` / `vmaf_p5`, and `psnr_y` / `ssim` when they were
measured. Only `kbps` and `vmaf` take part in the arithmetic; the rest is carried
so a rung can be judged on more than its mean.

With more than one clip a point also carries `clips` (how many it averages) and
`vmaf_spread` (the distance between the best and worst of them). Both are absent
for a single-clip run. Note that `results[]` holds the **per-clip** measurements
and `analysis[]` the aggregated ones: the raw numbers are never thrown away, so a
spread can always be traced back to which cut produced which end of it.

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

## The exported ladder

[`export`](cli.md#export) writes a different document: the recommended ladder
alone, in the shape another system accepts. In `json`:

| Field | Type | What it is |
|---|---|---|
| `encoder`, `codec`, `preset` | string | Which encoder's ladder this is. |
| `config_sha256`, `generated` | string | The run behind it, so a playlist in production can be traced to the measurement. |
| `target_vmaf`, `target_reached` | | What the top rung aimed at, and whether the grid got there. A ladder from a run that missed its target is still a ladder — it is just not the one that was asked for, and every format says so. |
| `rungs` | array | Best first, the order a master playlist is usually written in. |

**A rung**: `height`, `width` (derived, absent when there is no geometry to derive
it from), `target_kbps` (what the grid asked for), `peak_kbps` (the cap the encode
was given, which is what a playlist has to declare), `kbps` (what the file
measured) and `vmaf`.

## The comparison report

[`compare`](cli.md#compare) renders in the same three formats, over a different
shape. In `json`:

| Field | Type | What it is |
|---|---|---|
| `baseline`, `current` | object | How each run is identified: `path`, `version`, `generated`, `input`, `environment`. |
| `comparable` | bool | Whether the two runs measured the same experiment, i.e. whether their config fingerprints match. **When false, nothing else here answers "did this get worse"** — and the gate fails rather than passing. |
| `encoders` | array | One entry per encoder both runs measured. |
| `only_in_baseline`, `only_in_current` | array | Encoders one run has and the other does not. Named rather than dropped. |
| `regressions` | array | `encoder`, `reason`, and `value`/`threshold` for the ones that are a matter of degree. Empty when nothing regressed, and always empty when `comparable` is false. |
| `threshold` | number | The BD-rate percent the gate allowed. |

**An encoder entry**: `bd_rate` (the headline — bitrate for the baseline's
quality, positive means more expensive) and `bd_rate_by_height`, both the same
shape as a [BD figure](#bd_rates); `points` (per coordinate: `baseline_kbps`,
`kbps`, `baseline_vmaf`, `vmaf`, or a `note` when only one run measured it);
`ladder` (per rung, when the shapes match) or `ladder_changed` with
`baseline_ladder` and `current_ladder`; `baseline_target_reached` and
`target_reached`; and the two ladder totals.

```bash
# did anything regress, and why?
jq -r '.regressions[]? | "\(.encoder): \(.reason)"' comparison.json

# the headline per encoder, only if the runs are actually comparable
jq -r 'if .comparable then .encoders[] | "\(.encoder): \(.bd_rate.rate_pct)%"
       else "incomparable: the config fingerprints differ" end' comparison.json
```

### Reading it from a shell

```bash
ladder-bench run --output json --out ladder.json

# every measured point of one encoder, as bitrate/VMAF pairs
jq -r '.analysis[] | select(.encoder=="x264-fast")
       | .curves[] | .height as $h | .points[]
       | "\($h)p \(.kbps|round)k \(.vmaf)"' ladder.json

# every rung whose worst frames are far below its average
jq -r '.results[] | select(.vmaf.vmaf_p1 != null and .vmaf.vmaf_p1 < .vmaf.mean - 15)
       | "\(.height)p \(.target_kbps)k mean \(.vmaf.mean) p1 \(.vmaf.vmaf_p1)"' ladder.json

# which clip produced each end of the widest spread
jq -r '.results[] | "\(.clip // "-") \(.height)p \(.target_kbps)k VMAF \(.vmaf.mean)"' ladder.json | sort

# was everything measured by the same libvmaf?
jq -r '.environment.libvmaf | if length > 1 then "MIXED: \(.)" else "one build: \(.[0])" end' ladder.json

# every rung where the harmonic mean fell more than 2 points below the mean
jq -r '.results[] | select(.vmaf.harmonic_mean < .vmaf.mean - 2)
       | "\(.height)p \(.target_kbps)k mean \(.vmaf.mean) harmonic \(.vmaf.harmonic_mean)"' ladder.json

# the headline BD-rate
jq -r '.bd_rates[]? | "\(.test) vs \(.anchor): \(.frontier.rate_pct // "n/a")%"' ladder.json
```
