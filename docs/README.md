# ladder-bench documentation

Published as a site at **<https://allan-nava.github.io/ladder-bench/>**, built
from the Markdown in this directory. The files here are the only copy — the site
renders them, it does not duplicate them.

- **[index.md](index.md)** — overview: what the tool is, install, the first run,
  and the one rule it follows (it never invents a number).
- **[configuration.md](configuration.md)** — every key of `ladder-bench.yml`.
- **[method.md](method.md)** — what happens to a file and why: the lossless
  reference, the capped encodes, the VMAF upscale, and how the knee, the
  frontier, the recommended ladder and the BD-rate are computed. Read this
  before acting on a report — it also lists what the numbers do *not* cover.
- **[output.md](output.md)** — the report block by block, every phrase it can
  print, and the full JSON schema.
- **[cli.md](cli.md)** — every command, flag and exit code.
- **[docker.md](docker.md)** — the container image that already has an ffmpeg
  with libvmaf, and the flags that make a bind-mounted run behave.
- **[ci.md](ci.md)** — scheduled runs, what a runner needs (an ffmpeg with
  libvmaf), caching a work dir, job summaries.

Start with `ladder-bench init > ladder-bench.yml`, then `ladder-bench doctor`.

## How the site is built

`_config.yml`, `_layouts/default.html` and `assets/style.css` are the whole site
machinery: no theme, no gem beyond what GitHub Pages already runs, no build step
to install. [`.github/workflows/pages.yml`](../.github/workflows/pages.yml)
renders this directory with Jekyll and deploys it on every push to `main`.

**Adding a page is adding a file.** Give it front matter with a `title`, a
`nav_order` and a one-line `nav_blurb`, and the sidebar picks it up — there is no
navigation list to keep in sync somewhere else.

```yaml
---
title: Short name for the sidebar
nav_order: 7
nav_blurb: One line, shown under the link
description: >-
  A sentence for the page's meta description. Keep colons out of it or quote it.
---
```

Cross-page links stay plain relative Markdown (`method.md`), which works both on
github.com and on the site — `jekyll-relative-links` rewrites them at build
time. Links to files **outside** this directory must be absolute GitHub URLs;
the site only contains what is here.
