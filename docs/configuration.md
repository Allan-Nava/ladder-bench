---
title: Configuration
nav_order: 2
nav_blurb: Every key of ladder-bench.yml
description: >-
  Every key of ladder-bench.yml — the clip, the encoders, the resolution and bitrate grid, the VMAF filter and the analysis thresholds.
---

# Configuration

`ladder-bench init` writes a commented starting config. This page is the
reference for every key; the reasoning behind the defaults is in
[method.md](method.md).

Unknown keys are an **error**, not a warning: a typo would otherwise fall back
to the default and quietly change what was measured.

## Top level

| Key | Default | Meaning |
|---|---|---|
| `input` | — (required) | The source file. Use real content of the kind you ship. |
| `work_dir` | `.ladder-bench` | Where the reference clip, the encodes and the VMAF logs go. |
| `concurrency` | `1` | How many grid points encode at once. |
| `keep_encodes` | `false` | Keep the encoded files after the run. The VMAF logs are always kept. |
| `ffmpeg` / `ffprobe` | from `PATH` | Explicit binary paths. |

`concurrency: 1` is deliberate: the encoders already use every core, and
parallel runs make the per-point encode times meaningless. Raise it only if you
do not care about those timings.

## `clip`

```yaml
clip:
  start: "60s"
  duration: "30s"
```

Durations are **quoted strings** (`30s`, `1m30s`). A bare number is rejected:
`start: 60` reads as a minute to a human and as 60 nanoseconds to a duration
parser. Omitting `duration` uses the whole source, which is rarely what you
want — the clip is encoded once per grid point.

## `clips`

One clip answers "what does this ladder do to *these* thirty seconds". Several
answer the question you actually have:

```yaml
clips:
  - start: "0s"
    duration: "30s"
  - start: "12m"
    duration: "30s"
  - start: "38m"
    duration: "30s"
```

`clips` and `clip` are **mutually exclusive** — setting both would leave it
ambiguous which one was measured while the report looked identical either way —
and the same cut twice is an error, because it would halve the apparent
disagreement at every point the two land on.

Every clip is measured across the **whole grid**, so three clips triple the run.
In exchange the report gains a `SPREAD` column and an `across N clips` block: the
VMAF distance between the best and worst clip at each point, and the widest of
them. When that spread is wider than `ladder_step`, two cuts of your own source
disagree about a rung by more than a whole rung — which means the ladder is an
average of two different answers, and the honest next step is more content rather
than more confidence.

Everything downstream — the knee, the frontier, the recommended ladder, the
BD-rate — is computed on the **aggregated** curve. Bitrates and VMAF are averaged
across clips; `MIN`, `P5` and `P1` take the *worst* clip, because averaging tails
would hide the cut that fell apart, which is the cut the extra clips were
measured to find.

**Adding a second clip re-measures the first.** Output files carry the clip in
their name only when there is more than one, so a single-clip work dir keeps
working across upgrades — and going multi-clip renames those files, which
re-encodes them. That is the same rule as renaming an encoder: a run over three
cuts is not the run over one that came before it.

## `encoders`

```yaml
encoders:
  - name: x264-vod        # names the output files; must be unique
    codec: libx264        # any encoder your ffmpeg has
    preset: slow          # passed as -preset when set
    extra_args: ["-tune", "film"]   # appended last, so they win
```

Each encoder is measured across the **whole** grid, so two encoders double the
run. Encoders that spell their speed control differently can leave `preset`
empty and pass it through `extra_args`.

**Order matters when there is more than one.** The first encoder listed is the
**anchor** of the BD-rate comparison — the one you ship today — and every other
encoder is reported against it, so a negative percentage always means "the
challenger is cheaper". List the incumbent first.

## `rungs`

```yaml
rungs:
  - height: 1080
    bitrates: [2000, 3000, 4000, 5000, 6000]   # kbps
  - height: 720
    bitrates: [1000, 1500, 2000, 3000]
```

Heights must be even (yuv420p) and unique; bitrates are kbps and must be
unique within a rung. Bracket the range you might actually ship — the report
says explicitly when a curve was **still climbing** at the top of its grid or
**already flat** at the bottom, which is the signal to extend it.

## `vmaf`

```yaml
vmaf:
  model: version=vmaf_v0.6.1   # vmaf_4k_v0.6.1 for 4K content on 4K displays
  n_threads: 0                 # 0 = let libvmaf decide
  n_subsample: 1               # score every Nth frame
  metrics: [psnr, ssim]        # extra metrics, collected in the same pass
```

`n_subsample: 5` makes the measuring pass several times faster and is usually
good enough to shape a curve; keep it at 1 for a final number. It also coarsens
the **P5 and P1** columns, which are percentiles over the frames that were
actually scored — with a subsample of 5 they describe every fifth frame, not
every frame.

`metrics` asks libvmaf for extra quality metrics while it is already decoding
and aligning the frames, so each one costs a fraction of the VMAF pass it rides
along with. Two are available:

| Name | What lands in the report | Unit |
|---|---|---|
| `psnr` | Peak signal-to-noise ratio, **Y plane only** | dB |
| `ssim` | Structural similarity, libvmaf's float implementation | 0–1 |

They are **off by default**, and not because of the cost. Turning them on makes
every VMAF log already in the work dir stale: a log written without them cannot
be made to contain them, so those points are encoded and measured again the next
time you run. Switch them on when you set up a grid, not halfway through one.

Only these two names are accepted. libvmaf exposes many more features, but a
metric this tool cannot label or explain in the report is a column of numbers
nobody can act on — a typo is an error naming the alternatives, never a silently
dropped request.

**VMAF still decides everything.** The knee, the frontier, the recommended
ladder and the BD-rate are all computed from VMAF alone; PSNR and SSIM are there
to be checked against when a VMAF number looks surprising. See
[Method](method.md#4-the-analysis).

## `analysis`

```yaml
analysis:
  knee_gain: 0.5      # VMAF points per +10% bitrate below which a rung is flat
  target_vmaf: 93.0   # what the top rung aims at
  ladder_step: 6.0    # VMAF distance between rungs
  gop_seconds: 2.0    # keyframe interval used for every encode
```

`target_vmaf` around 93 is a common "looks right on a big screen" target; 95+
starts paying for bits few viewers can see. Set `gop_seconds` to your segment
duration.

## `current_ladder`

```yaml
current_ladder:
  - height: 1080
    bitrate: 5000
  - height: 720
    bitrate: 3000
```

Optional. When set, the report says what each rung delivers today and what the
same quality costs on the frontier. Rungs at a resolution the grid never
measured are reported as such and excluded from the totals.

## Flags

`run` takes `--config`, `--input`, `--output text|markdown|json`, `--out FILE`,
`--concurrency N`, `--force`, `--verbose`, `--quiet`. `--input` and
`--concurrency` override the config, which is what you want when the same
config is run against several sources.
