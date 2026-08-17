# ladder-bench documentation

- **[method.md](method.md)** — what the tool does to a file and why: the
  lossless reference, the capped encodes, the VMAF upscale, and how the knee,
  the frontier and the recommended ladder are computed. Read this before acting
  on a report — it also lists what the numbers do *not* cover.
- **[configuration.md](configuration.md)** — every key of `ladder-bench.yml`
  and every flag.
- **[ci.md](ci.md)** — running it on a schedule, caching a work dir between
  runs, and what a runner needs (an ffmpeg with libvmaf).

Start with `ladder-bench init > ladder-bench.yml`, then `ladder-bench doctor`.
