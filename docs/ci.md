# Running it in CI

ladder-bench is a batch tool, not a service: it fits a scheduled job or a
manual dispatch far better than a per-PR check. A grid is minutes of CPU at
best, and the answer only changes when the content, the encoder or the target
changes.

## What a runner needs

- **ffmpeg built with libvmaf.** Most distro packages are not. On GitHub-hosted
  runners, install a static build or use a container image that has it.
  `ladder-bench doctor` fails loudly and early if it is missing.
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

jobs:
  bench:
    runs-on: ubuntu-latest
    container: jrottenberg/ffmpeg:7-ubuntu # an image that ships libvmaf
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25" }
      - run: go install github.com/Allan-Nava/ladder-bench/cmd/ladder-bench@latest
      - run: curl -fsSL "$CLIP_URL" -o clip.mp4
        env: { CLIP_URL: "${{ secrets.BENCH_CLIP_URL }}" }
      - run: ladder-bench doctor --config bench/ladder-bench.yml --input clip.mp4
      - name: Measure
        run: |
          ladder-bench run --config bench/ladder-bench.yml --input clip.mp4 \
            --output markdown --quiet >> "$GITHUB_STEP_SUMMARY"
      - name: Keep the raw measurements
        run: ladder-bench run --config bench/ladder-bench.yml --input clip.mp4 --output json --out ladder.json
      - uses: actions/upload-artifact@v4
        with:
          name: ladder-bench
          path: ladder.json
```

The second `run` costs nothing: every point is already on disk, so it only
re-renders the report in another format.

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
committed baseline — is [LB-13](../BACKLOG.md) and not implemented yet.
