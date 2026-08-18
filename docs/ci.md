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

## Failing on a regression

A scheduled report tells you what the ladder is. A **gate** tells you when it
changed for the worse, which is the only part a pipeline can act on by itself.

Commit a baseline report next to the config, and compare against it:

```yaml
      - name: ladder-bench
        run: |
          lb() {
            docker run --rm -v "$PWD:/work" --user "$(id -u):$(id -g)" "$LB" "$@"
          }
          lb run --config bench/ladder-bench.yml --input clip.mp4 \
            --output json --out current.json --quiet
          lb compare bench/baseline.json current.json --output markdown \
            >> "$GITHUB_STEP_SUMMARY"
          lb compare bench/baseline.json current.json --exit-on-regression --quiet
```

The second `compare` costs nothing — it re-reads two JSON files — and its exit
code is what fails the job: `2` for a regression, `1` if the gate could not be
applied at all. See [`compare`](cli.md#compare) for what counts as one.

Three things make this work rather than annoy:

- **The baseline is committed.** It is a file in the repository, so refreshing it
  is a reviewed change — which is exactly what accepting a new normal should be.
- **The config fingerprint is checked first.** Editing the config invalidates the
  baseline, and the gate then *fails* rather than passing on a comparison it
  could not make. That is the signal to re-measure the baseline in the same pull
  request that changed the config.
- **The threshold is not zero.** Encoders are not bit-exact across runs; a gate at
  zero fails on noise and gets switched off.

Distinguishing the two failure codes is worth the extra line:

```yaml
      - name: Gate
        run: |
          set +e
          docker run --rm -v "$PWD:/work" --user "$(id -u):$(id -g)" "$LB" \
            compare bench/baseline.json current.json --exit-on-regression
          case $? in
            0) echo "no regression" ;;
            2) echo "::error::the ladder regressed against the committed baseline"; exit 1 ;;
            *) echo "::error::the gate could not run"; exit 1 ;;
          esac
```

## Exit codes

`0` when the run completed, non-zero for a real failure (bad config, missing
libvmaf, a point that could not be encoded or measured). A broken point stops
the run: a hole in the curve is not a smaller answer, it is a wrong one.

`compare --exit-on-regression` exits `2` when the ladder got worse against the
baseline, and `1` when it could not tell.
