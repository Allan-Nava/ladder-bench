---
title: Method
nav_order: 3
nav_blurb: What it does to a file, and why
description: >-
  What ladder-bench does to a file and why each step is the way it is — the lossless reference clip, the capped encodes, the VMAF upscale, and how saturation, the efficient frontier, the recommended ladder and the BD-rate are computed.
---

# Method

What ladder-bench does to a file, and why each step is the way it is. If you
are going to act on the numbers, this is the page that says whether you should.

## 1. The reference clip

```
ffmpeg -ss <start> -i <input> -t <duration> -map 0:v:0 -an -sn -dn \
       -c:v libx264 -preset ultrafast -crf 0 -pix_fmt yuv420p ref.mkv
```

The clip is cut **once**, and every encode and every comparison uses it.

- **Lossless, not `-c copy`.** A stream copy can only cut on a keyframe, so the
  clip would start at a different frame than you asked for and keep the
  source's timestamps. Every measurement downstream would then be over a
  slightly different set of frames.
- **`-pix_fmt yuv420p`.** The VMAF models are trained on 8-bit 4:2:0. Feeding
  mixed pixel formats makes libvmaf convert implicitly — silently, and
  differently per input.
- **`-ss` before `-i`.** Seeks by index instead of decoding to the cut point.

The clip's file name carries the cut (`reference_60s_30s.mkv`), so changing
`clip:` produces a new file instead of quietly reusing the previous one.

**Pick the clip like a measurement, not like a thumbnail.** A static intro
makes every rung look brilliant; 30–60 seconds of the busiest content you
actually ship is the useful choice. The clip is paid once per grid point.

**Better still, pick several.** [`clips:`](configuration.md#clips) measures the
whole grid against each of them and reports how far apart they came out. On a
synthetic source built from three deliberately different cuts — flat colour,
detail, pure noise — the same 720p rung measured VMAF 97, 76 and 15: a spread of
**82 VMAF at one grid point**. A single clip would have reported whichever of
those three it happened to be, with no hint that the other two existed.

## 2. The encodes

```
ffmpeg -i ref.mkv -vf scale=-2:<height>:flags=lanczos \
       -c:v <codec> -preset <preset> \
       -b:v <kbps>k -maxrate <110%>k -bufsize <200%>k \
       -g <gop> -keyint_min <gop> -pix_fmt yuv420p -an -sn -dn out.mp4
```

- **Capped rate control.** A rendition that overshoots its declared bandwidth
  is a rendition the player abandons mid-stream. Measuring an uncapped encode
  would report quality nobody can actually deliver at that rung.
- **Fixed GOP** (`gop_seconds`, default 2s × the clip's frame rate). Match your
  segment duration: longer GOPs report an efficiency an ABR delivery cannot
  ship.
- **`scale=-2`** keeps the aspect ratio and rounds the width to an even number,
  which yuv420p requires.
- **`extra_args` are appended last**, so anything you add wins over the
  defaults above.

## 3. The measurement

```
ffmpeg -i out.mp4 -i ref.mkv -lavfi \
  "[0:v]scale=<refW>:<refH>:flags=bicubic,setpts=PTS-STARTPTS[dist];\
   [1:v]setpts=PTS-STARTPTS[ref];[dist][ref]libvmaf=log_fmt=json:log_path=…" \
  -f null -
```

Two details decide whether the score means anything, and neither fails loudly
when you get it wrong:

- **The distorted encode is input 0, the reference input 1.** Swapping them
  produces a different, wrong number — not an error.
- **The distorted encode is scaled back up to the reference resolution.** VMAF
  models a viewer watching on a full-size screen, so a 360p rung must be judged
  as the upscaled picture the viewer sees. Comparing at native resolution makes
  low rungs look excellent and makes the cross-resolution comparison — the
  whole point of a ladder — meaningless.

The reported bitrate is the **real** one (file size over clip duration), not
the one that was requested: rate control never lands exactly on target.

### What comes out of one pass

libvmaf pools more than a mean, and every log carries all of this:

- **VMAF mean** — the number every recommendation in the report is computed
  from.
- **VMAF harmonic mean** — the same frames, weighted so the worst ones count for
  more. When it sits well below the mean, a few seconds of the clip fell apart
  and the average absorbed it. A rung whose mean and harmonic mean agree is a
  rung that was consistently that good.
- **VMAF minimum** — the single worst frame.
- **P5 and P1** — the 5th and 1st percentile of the per-frame scores. The mean is
  designed to absorb bad moments; these are the columns that refuse to. A rung
  averaging 93 with a P1 of 70 is not a rung that looks like 93.

Percentiles use **nearest rank**: the value at position ceil(p/100 · N) of the
sorted frames, so a reported P1 is always a score some frame actually received.
An interpolated percentile would be a number no frame was given, which is the
kind of value this tool refuses to print everywhere else. Two consequences worth
knowing: `n_subsample` coarsens them, because the percentile is taken over the
frames that were *scored* rather than all of them; and on a short clip P1 and P5
collapse onto the same frame, which is honest rather than broken — twenty frames
cannot distinguish a 1st from a 5th percentile.

Adding `vmaf.metrics: [psnr, ssim]` collects two more in the **same pass**, for a
fraction of its cost: the frames are already decoded, scaled and aligned, so the
extra work is arithmetic on pixels libvmaf is holding anyway.

- **PSNR (Y plane, dB)** — pure signal error, no perceptual model. It notices
  things VMAF forgives and forgives things VMAF notices, which is exactly why it
  is worth having next to it.
- **SSIM (0–1)** — structural similarity, libvmaf's float implementation.

They are reported, never acted on: the knee, the frontier, the recommended ladder
and the BD-rate all come from VMAF alone. Two metrics disagreeing is information;
averaging them into one score would only hide which one you were trusting.

Asking for them **invalidates the logs already on disk** — a log written without
PSNR cannot be made to contain it — so those points get measured again. The
alternative would be printing an empty column, which reads as a measurement that
came back blank rather than one that was never taken.

## 4. The analysis

All of it is arithmetic over the measured points — no model, no fitting.

**Saturation (the knee).** For each resolution, the gain of a step is expressed
as *VMAF points per +10% bitrate*, because +500 kbps means something entirely
different at 800 kbps and at 6000 kbps. The knee is the last point whose bits
still buy quality: past it, every step gains less than `knee_gain`. The scan
runs from the top down — a real curve has noise, and a bottom-up scan stops at
the first flat step and reports a knee far below the real one.

**The efficient frontier (convex hull).** Pool every point of every resolution
and keep the upper-left boundary. That answers "at this bitrate, which
resolution looks best?", which is exactly what a ladder encodes, and why a
well-encoded 720p rung can outrank a starved 1080p one. Points that cost more
and score less are dropped: as advice they would read "pay more, see less".

**The recommended ladder.** Rungs are taken **from the frontier** — every rung
is a point that was really measured, never an interpolated bitrate — and spaced
by `ladder_step` VMAF (~6, roughly one just-noticeable difference). Equal
bitrate steps instead pile up indistinguishable rungs at the top and leave a
cliff at the bottom. The top rung is the *cheapest* point reaching
`target_vmaf`, not the best point measured.

**Across clips.** Points measured on several cuts are folded into one point per
grid coordinate before any of the above runs, so the knee, the frontier and the
ladder describe all of the measured content rather than three curves that would
have to be reconciled. Bitrate and VMAF are averaged; `MIN`, `P5` and `P1` take
the worst clip, because averaging tails hides the cut that fell apart. The
dispersion is reported rather than folded away: when the spread at a point
exceeds `ladder_step`, the report says that the clip choice mattered more than
the rung choice.

**Versus your current ladder.** For each configured rung: the quality it
delivers today (interpolated within its own resolution's measured curve), and
the cheapest place on the frontier that reaches the same quality. The totals
sum **only the rungs that could be measured** — putting an unmeasured rung on
one side and nothing on the other would invent a saving out of a missing
measurement. A rung within 5% of the ends of the measured range is snapped to
the end (rate control never lands on target); further out it is reported as
outside the grid rather than extrapolated.

**BD-rate (two encoders or more).** The Bjøntegaard delta rate answers "how much
bitrate does the challenger need for the same quality?" in one percentage, so a
codec decision does not come down to picking a favourite operating point. The
anchor is the **first encoder listed in the config** — the one you ship today —
and negative always means fewer bits.

Both curves are integrated as **log10(bitrate) over VMAF**: bitrate differences
are multiplicative, so averaging them linearly would let the expensive end of
the grid outvote the cheap end. With four or more points a curve gets the
classic least-squares cubic fit; with two or three it is interpolated between
the measurements instead, and the report says which — a cubic through three
points is not a fit, it is a curve drawn through noise. The quality axis is
centred before fitting, because VMAF sits around 90 and an uncentred cubic loses
most of its precision to conditioning.

The average is taken over the quality range **both** encoders reached, never
over the union of the two: outside the overlap there is nothing to compare
against. Curves sharing less than 1.0 VMAF are declined rather than stretched to
meet — a percentage taken over a sliver looks like a verdict and is really the
noise of a couple of frames. The report gives a BD-rate per resolution and one
between the two efficient frontiers; the frontier figure is the ladder-level
answer, where each encoder is free to pick its best resolution at each bitrate.

## 5. What the report records about itself

A measurement you cannot place is a measurement you can only trust or distrust,
never re-check. Every report carries:

- **the ffmpeg version line**, verbatim as the binary printed it. Distributions
  and static builds spell it differently, and quoting what it said beats storing
  this tool's interpretation of it;
- **every libvmaf version that wrote one of the logs**, as a list. A list because
  a resumed grid can mix two: points measured before an ffmpeg upgrade and points
  measured after it are not the same experiment, and the report says so out loud
  rather than averaging over it;
- **a SHA-256 fingerprint of the resolved config** — after defaults have been
  applied, so a file that spells out what another leaves implicit fingerprints
  the same. The work dir, the binary paths, `concurrency` and `keep_encodes` are
  deliberately excluded: they change where and how fast a run happens, never what
  it measures, and two machines with different paths have to agree on the hash for
  it to be worth anything.

## 6. Comparing two runs

One report says what the ladder is. The question that follows is always whether
that changed — a new ffmpeg, a new preset, a different week of content — and
[`compare`](cli.md#compare) answers it with the same BD-rate arithmetic pointed at
time instead of at a competitor: **how much bitrate this run needs for the quality
the baseline delivered**.

Everything that makes the cross-encoder BD-rate honest applies here too: the
average covers only the quality range both runs reached, curves that barely
overlap are declined, and nothing is interpolated between the two.

Two extra refusals belong to this comparison specifically:

- **Two runs of different experiments are not compared, they are reported as
  different.** The config fingerprint decides. A wider grid, another target or one
  more clip move every number without anything having got better or worse.
- **A ladder whose shape changed is not paired rung by rung.** Two ladders with
  different resolutions are shown side by side, because pairing them by index
  would invent a comparison.

The totals, meanwhile, stay as untrustworthy as they were: a real comparison in
these docs shows a ladder total falling while every rung got worse. Rate control
still hit its targets; what changed is what those bits bought. That is why the
headline is a BD-rate and not a sum.

## 7. Handing the result on

A ladder that has to be retyped into a packager is a ladder that will eventually
be retyped wrong, so [`export`](cli.md#export) writes it as a master playlist, a
DASH adaptation set or JSON.

Two things it will not do. It will not **invent a `CODECS` string**: that carries
the profile and level the encoder chose, which is not measured here, and a guessed
one produces a playlist players reject in ways that look like content problems. And
it will not **invent a `RESOLUTION`**: the width is derived from the reference
geometry the way `scale=-2:H` derives it, and omitted when there is no geometry to
derive it from.

What it does declare carefully is the bitrate. An HLS `BANDWIDTH` is a *peak*, and
the peak of these encodes is the cap they were given — 110% of target — not the
average they came out at. Both numbers are exported, as `BANDWIDTH` and
`AVERAGE-BANDWIDTH`, because a packager needs the first and a capacity plan needs
the second.

[`chart`](cli.md#chart) draws the same measurements as an SVG: one line per
resolution, the frontier as an envelope over them, the knees ringed and the target
as a dashed line. The bitrate axis is logarithmic, because a doubling is the same
decision wherever it happens on the curve.

## What this does not measure

- **Your audience.** The savings are per-rung and per-quality, not weighted by
  how many viewers sit on each rung. A CDN bill needs that distribution.
- **Anything beyond the clip.** One 30-second clip is one 30-second clip; run
  several and compare before betting a season on the result.
- **Encoder speed as a trade-off.** Encode times are recorded, but the analysis
  optimises quality per bit, not quality per CPU-hour.
- **Hardware encoders on equal terms.** They can be put in the grid, but a
  quality-per-bit comparison against a software encoder is not a fair fight.
