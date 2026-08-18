---
title: CLI reference
nav_order: 5
nav_blurb: Commands, flags, exit codes
description: >-
  Every ladder-bench subcommand and flag — init, doctor, plan, run and version —
  with the exit codes, what interrupting a run leaves behind, and what ends up
  in the work directory.
---

# CLI reference

```
ladder-bench init   [--out FILE] [--force]
ladder-bench doctor [--config FILE] [--input FILE]
ladder-bench plan   [--config FILE] [--input FILE]
ladder-bench run    [--config FILE] [--input FILE] [--output FORMAT] [--out FILE]
                    [--concurrency N] [--force] [--verbose] [--quiet]
ladder-bench version
ladder-bench help
```

Flags come **after** the subcommand, and the Go standard library accepts both
`-flag` and `--flag`, with either `--flag value` or `--flag=value`. There are no
environment variables and no global flags: everything a run does comes from the
config file, plus the handful of overrides below.

`ladder-bench <command> --help` prints the flags of that command.

## `init`

Writes a commented starting config — the same one embedded in the binary, so it
can never drift from the defaults the code applies.

| Flag | Default | What it does |
|---|---|---|
| `--out FILE` | *stdout* | Write to a file instead of standard output. |
| `--force` | `false` | Overwrite `--out` if it already exists. Without it, an existing file is an error rather than a loss. |

```bash
ladder-bench init > ladder-bench.yml
ladder-bench init --out bench/ladder-bench.yml
```

Every key it writes is documented in [Configuration](configuration.md).

## `doctor`

Checks everything that otherwise fails **late**, and prints one line per check.

- the config parses, and how many points and encoders it asks for;
- `ffmpeg` and `ffprobe` are on `PATH` (or wherever the config points);
- this ffmpeg has the **libvmaf** filter — a build-time option, so a healthy
  ffmpeg may simply not have it;
- every configured `codec:` is an encoder this build actually has;
- the source file can be probed, with its geometry, frame rate and duration;
- `work_dir` exists and is writable.

| Flag | Default | What it does |
|---|---|---|
| `--config FILE` | `ladder-bench.yml` | Config to check. |
| `--input FILE` | *from config* | Check this source instead of the one in the config. |

```
[ok  ] config ladder-bench.yml: 12 points, 2 encoder(s)
[ok  ] ffmpeg /opt/homebrew/bin/ffmpeg
[ok  ] libvmaf filter available
[ok  ] encoder "x264-fast": libx264 available
[ok  ] input source.mp4: 1920x1080 @ 25.00 fps, h264, 6.0s
[ok  ] work_dir .ladder-bench is writable

All good — 'ladder-bench run' has everything it needs.
```

A failed check prints `[FAIL]` with what to do about it and the command exits
non-zero. An unusable config or a missing ffmpeg stops the checks there — there
is nothing left to check against.

## `plan`

Prints what a run *would* do, without touching a single frame: the grid, the
disk estimate, and the **exact** ffmpeg command line for the reference cut and
for every encode and measurement.

| Flag | Default | What it does |
|---|---|---|
| `--config FILE` | `ladder-bench.yml` | Config to plan. |
| `--input FILE` | *from config* | Plan against this source instead. |

This exists because the commands *are* the method: a benchmark whose encoder
settings you cannot read is a benchmark you cannot argue with. It is also how to
check a config change without paying for the encodes.

`plan` probes the source when ffmpeg is available, because the reference
geometry decides the VMAF scaling. When it cannot, it says so and prints a line
beginning `# ASSUMED reference geometry` — a plan printed with assumed geometry
is a plan, not a measurement.

## `run`

Cuts the reference clip, encodes every grid point, measures each one with VMAF,
and writes the report.

| Flag | Default | What it does |
|---|---|---|
| `--config FILE` | `ladder-bench.yml` | Config to run. |
| `--input FILE` | *from config* | Measure this source instead of the one in the config. |
| `--output FORMAT` | `text` | `text`, `markdown` or `json`. See [The report](output.md). |
| `--out FILE` | *stdout* | Write the report to a file. Progress still goes to stderr. |
| `--concurrency N` | *from config* (`1`) | How many encodes run at once. |
| `--force` | `false` | Re-encode and re-measure points whose files are already on disk. |
| `--verbose` | `false` | Echo every ffmpeg command to stderr as it runs. |
| `--quiet` | `false` | No per-point progress on stderr. |

The report goes to **stdout**, progress and errors to **stderr**, so
`ladder-bench run --output markdown >> "$GITHUB_STEP_SUMMARY"` does the right
thing without `--quiet`.

`--concurrency` is worth leaving at `1`. ffmpeg already uses every core, so
parallel encodes mostly make the per-point timings meaningless — and those
timings are the only cost signal the report carries.

### Resuming, and when to force

A point is reused when both its encode and its VMAF log are already on disk, so
an interrupted grid picks up where it stopped and re-rendering the same
measurements in another format costs nothing.

Four consequences worth knowing before trusting a cached work dir:

- **Renaming an encoder invalidates its points**, because the output files are
  named after it. Changing a preset **without** renaming does *not* — use
  `--force`, or rename the encoder, whenever you change what it does.
- **Changing `clip:` is safe.** The reference file name carries the cut
  (`reference_60s_30s.mkv`), so a new cut is a new file rather than a silent
  reuse of the old one.
- **Adding a second clip re-measures the first.** The clip appears in the file
  names only when there is more than one, so a single-clip work dir survives
  every upgrade — and going multi-clip renames those files, which re-encodes
  them. Same rule as renaming an encoder.
- **Adding to [`vmaf.metrics`](configuration.md#vmaf) re-measures by itself**,
  without `--force`. A log written before PSNR was asked for does not contain it
  and never will, so those points are encoded and measured again. Nothing is
  silently reused with a column left blank.

### The work directory

`work_dir` (default `.ladder-bench`) holds three kinds of file:

| File | Kept after a run | What it is |
|---|---|---|
| `reference_<start>_<duration>.mkv` | yes | A lossless reference clip, cut once and used by every encode and every comparison against it. One per entry in [`clips:`](configuration.md#clips). |
| `<encoder>_<height>p_<kbps>k.mp4` | only with `keep_encodes: true` | One grid point's encode. These are the bulk of the disk. With several clips the name becomes `<encoder>_<clip>_<height>p_<kbps>k.mp4`. |
| `<encoder>_<height>p_<kbps>k.vmaf.json` | yes | The libvmaf log — the measurement itself, which is why it survives the cleanup. |

### Interrupting a run

`Ctrl-C` (or `SIGTERM`) cancels the run and kills the ffmpeg it is waiting on.
Points that already finished stay on disk for the next run to reuse, so a long
grid is safe to stop.

A **broken** point stops the run too, and the error names the point and carries
the tail of ffmpeg's own output. This is deliberate: a hole in the curve is not
a smaller answer, it is a wrong one.

The tail is deep enough to clear ffmpeg's cascade — one message per thread that
noticed — and when the line that actually explains the failure came from the
encoder library rather than from ffmpeg, it is **lifted above** the tail with a
`…` marking the gap:

```
ladder-bench: svt-av1 720p @ 3000k: encode: ffmpeg: exit status 234
Svt[error]: Instance 1: Max Bitrate only supported with CRF mode
…
[vost#0:0/libsvtav1 @ …] Could not open encoder before EOF
[out#0/mp4 @ …] Nothing was written into output file
```

Without that, the cause is the first thing a fixed-size tail throws away.

## `version`

```bash
ladder-bench version     # also --version, -v
```

Prints `ladder-bench <version>`. Release builds have the tag injected at build
time; a `go build` from source reports `dev`.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The command completed. For `doctor`, every check passed. |
| `1` | A real failure: unusable config, missing ffmpeg or libvmaf, a failed check, a point that could not be encoded or measured, an unwritable report. |
| `64` | Usage error — no subcommand, or one that does not exist. Usage goes to stderr. |

Gating a pipeline on a *regression* — the recommended ladder getting worse
against a committed baseline — is
[LB-13](https://github.com/Allan-Nava/ladder-bench/blob/main/BACKLOG.md) and not
implemented yet. Today a pipeline can fail on a broken run, not on a worse
result.
