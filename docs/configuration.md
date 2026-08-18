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
```

`n_subsample: 5` makes the measuring pass several times faster and is usually
good enough to shape a curve; keep it at 1 for a final number.

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
