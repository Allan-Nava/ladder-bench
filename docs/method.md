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

**Versus your current ladder.** For each configured rung: the quality it
delivers today (interpolated within its own resolution's measured curve), and
the cheapest place on the frontier that reaches the same quality. The totals
sum **only the rungs that could be measured** — putting an unmeasured rung on
one side and nothing on the other would invent a saving out of a missing
measurement. A rung within 5% of the ends of the measured range is snapped to
the end (rate control never lands on target); further out it is reported as
outside the grid rather than extrapolated.

## What this does not measure

- **Your audience.** The savings are per-rung and per-quality, not weighted by
  how many viewers sit on each rung. A CDN bill needs that distribution.
- **Anything beyond the clip.** One 30-second clip is one 30-second clip; run
  several and compare before betting a season on the result.
- **Encoder speed as a trade-off.** Encode times are recorded, but the analysis
  optimises quality per bit, not quality per CPU-hour.
- **Hardware encoders on equal terms.** They can be put in the grid, but a
  quality-per-bit comparison against a software encoder is not a fair fight.
