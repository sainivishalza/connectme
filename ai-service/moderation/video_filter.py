"""Video screening by sampling frames and running each through the image checks.

The clip is decoded with PyAV (ffmpeg libraries bundled in its wheel), a handful
of frames are taken evenly across it, and each frame goes through
check_image(), so the known-bad-hash list and the nudity model apply exactly as
they do for photos. The strictest frame decides: any block -> block, any
review -> flag, otherwise allow.

Honest scope: frames are sampled, so a short insertion between samples can be
missed, and audio is not analysed. It is a working first line of defence and a
review feed, not a certified scanner. With no image model configured it
refuses to answer (RuntimeError) so the caller holds the clip instead of
passing it unscreened.
"""
from __future__ import annotations

import io
import os
import tempfile

from PIL import Image

from moderation import image_filter

MAX_BYTES = 60 * 1024 * 1024
MAX_SECONDS = 120.0
SAMPLES = int(os.environ.get("VIDEO_SAMPLE_FRAMES", "8"))


class Unreadable(Exception):
    pass


def _frames(path: str, n: int) -> list[Image.Image]:
    import av  # imported lazily: only the video endpoint needs it

    try:
        with av.open(path) as c:
            vs = next((s for s in c.streams if s.type == "video"), None)
            if vs is None:
                raise Unreadable("no video stream")
            dur = float(c.duration) / av.time_base if c.duration else 0.0
            if dur > MAX_SECONDS:
                raise Unreadable("clip is too long")
            frames: list[Image.Image] = []
            if dur <= 0:
                # Unknown duration: take the first frames we can decode.
                for i, f in enumerate(c.decode(vs)):
                    frames.append(f.to_image())
                    if i + 1 >= n:
                        break
                return frames
            for k in range(n):
                t = dur * (k + 0.5) / n
                c.seek(int(t * av.time_base), backward=True, any_frame=False)
                for f in c.decode(vs):
                    frames.append(f.to_image())
                    break
            return frames
    except Unreadable:
        raise
    except Exception as e:  # av raises many error types for bad containers
        raise Unreadable("could not decode the clip") from e


def _png(img: Image.Image) -> bytes:
    img = img.convert("RGB")
    img.thumbnail((640, 640))
    b = io.BytesIO()
    img.save(b, "PNG")
    return b.getvalue()


def check_video(data: bytes) -> tuple[str, str]:
    """Returns (action, reason) with action in allow | flag | block."""
    if not data or len(data) > MAX_BYTES:
        return "block", "The video is empty or too large."
    if not image_filter.has_classifier():
        raise RuntimeError("no image model configured; refusing to pass a video unscreened")
    with tempfile.NamedTemporaryFile(suffix=".bin", delete=False) as f:
        f.write(data)
        path = f.name
    try:
        frames = _frames(path, SAMPLES)
    except Unreadable as e:
        return "block", f"The video could not be checked ({e})."
    finally:
        try:
            os.unlink(path)
        except OSError:
            pass
    if not frames:
        return "block", "No frames could be read from the video."
    worst = "allow"
    for fr in frames:
        v = image_filter.check_image(_png(fr))
        if v.action == "block":
            return "block", "A frame of this video breaks our community rules."
        if v.action == "review":
            worst = "flag"
    return worst, ("A frame of this video needs a quick review." if worst == "flag" else "ok")
