---
title: In CI
nav_order: 7
nav_blurb: Scheduled runs and caching
description: >-
  Running ladder-bench on a schedule — what a runner needs, a workflow that posts the report to a job summary, caching a work dir between runs, and the exit codes.
---

# Running it in CI

ladder-bench is a batch tool, not a service: it fits a scheduled job or a
manual dispatch far better than a per-PR check. A grid is minutes of CPU at
best, and the answer only changes when the content, the encoder or the target
changes.

## What a runner needs

- **ffmpeg built with libvmaf.** No distro package is — see the table in
  [Docker](docker.md#why-an-image-exists-at-all). Either fetch a static build on
  the runner, or run the job in a container that already has one:
  `ghcr.io/allan-nava/ladder-bench` ships ffmpeg, ffprobe and the CLI together.
  `ladder-bench doctor` fails loudly and early if libvmaf is missing.
- **The source media.** Keep a small, representative clip somewhere the job can
  fetch it — an object store, a release asset — rather than the full master.
- **Disk.** `ladder-bench plan` prints the estimate before anything runs.

## A scheduled report

```yaml
name: Ladder
on:
  workflow_dispatch:
  schedule:
    - cron: "0 3 1 * *" # monthly is plenty

env:
  # Pin the exact version. Comparing this month's report against last month's
  # only means something if the same code measured both.
  LB: ghcr.io/allan-nava/ladder-bench:0.4.0

jobs:
  bench:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: curl -fsSL "$CLIP_URL" -o clip.mp4
        env: { CLIP_URL: "${{ secrets.BENCH_CLIP_URL }}" }

      - name: ladder-bench
        run: |
          lb() {
            docker run --rm -v "$PWD:/work" --user "$(id -u):$(id -g)" "$LB" "$@"
          }
          lb doctor --config bench/ladder-bench.yml --input clip.mp4
          lb run --config bench/ladder-bench.yml --input clip.mp4 \
            --output markdown --quiet >> "$GITHUB_STEP_SUMMARY"
          lb run --config bench/ladder-bench.yml --input clip.mp4 \
            --output json --out ladder.json --quiet

      - uses: actions/upload-artifact@v4
        with:
          name: ladder-bench
          path: ladder.json
```

The second `run` costs nothing: every point is already on disk, so it only
re-renders the report in another format.

**`docker run`, not `container:`.** The image is Alpine-based, and a job
`container:` has to run GitHub's own Node.js for every JavaScript action —
`actions/checkout` included — which is a glibc build that does not start on musl.
Invoking the image per command sidesteps that entirely, and `--user` keeps the
work dir owned by the runner user so `upload-artifact` can read it.

If you would rather build from source, add `actions/setup-go` and
`go install github.com/Allan-Nava/ladder-bench/cmd/ladder-bench@latest`, and
bring your own ffmpeg with libvmaf.

## Caching between runs

Point `work_dir` at a cached directory and a re-run only measures what changed
— new grid points, a new encoder. Two rules make the cache safe:

- **Renaming an encoder invalidates its points**, because the output files are
  named after it. Changing a preset **without** renaming the encoder does not:
  use `--force`, or rename the encoder, whenever you change what it does.
- **Changing `clip:` is safe**: the reference file name carries the cut, so a
  new cut is a new file.

## Exit codes

`0` when the run completed, non-zero for a real failure (bad config, missing
libvmaf, a point that could not be encoded or measured). A broken point stops
the run: a hole in the curve is not a smaller answer, it is a wrong one.

Gating a pipeline on a *regression* — the ladder getting worse against a
committed baseline — is [LB-13](https://github.com/Allan-Nava/ladder-bench/blob/main/BACKLOG.md) and not implemented yet.
