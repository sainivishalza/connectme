"""Image moderation: structural checks, known-bad perceptual hashes, and an
optional pluggable classifier.

Honest scope: with no classifier configured this does NOT detect nudity or
violence. The response says so (`screened_by_model=False`) so callers and
operators cannot mistake it for full screening. CSAM detection requires a
licensed hash-matching vendor (PhotoDNA / NCMEC) and is done by the API
before this service is called; the local hash list here is an additional
blocklist of operator-supplied perceptual hashes.
"""
from __future__ import annotations

import io
import os
from typing import Callable

from PIL import Image, UnidentifiedImageError

from schemas import ImageVerdict

MAX_BYTES = 10 * 1024 * 1024
MAX_PIXELS = 40_000_000
HAMMING_THRESHOLD = 5
# Violence classifiers are far less reliable than nudity ones (sport, costumes
# and film stills trigger them), so by default a violence score only ever sends
# an image to a human; it never blocks on its own. Set VIOLENCE_BLOCK below 1
# to let a very confident score block.
VIOLENCE_REVIEW = float(os.environ.get("VIOLENCE_REVIEW", "0.7"))
VIOLENCE_BLOCK = float(os.environ.get("VIOLENCE_BLOCK", "1.01"))
Image.MAX_IMAGE_PIXELS = MAX_PIXELS

# A classifier maps an image to category scores in [0, 1], e.g.
# {"nudity": 0.02, "violence": 0.0}. Plug a real model in with set_classifier.
Classifier = Callable[[Image.Image], dict[str, float]]

_classifier: Classifier | None = None


def set_classifier(c: Classifier | None) -> None:
    global _classifier
    _classifier = c


def has_classifier() -> bool:
    return _classifier is not None


def average_hash(img: Image.Image, size: int = 8) -> int:
    """64-bit average hash: robust to resizing and recompression."""
    g = img.convert("L").resize((size, size), Image.Resampling.LANCZOS)
    px = list(g.tobytes())
    avg = sum(px) / len(px)
    bits = 0
    for p in px:
        bits = (bits << 1) | (1 if p >= avg else 0)
    return bits


def _hamming(a: int, b: int) -> int:
    return (a ^ b).bit_count()


def _blocklist() -> list[int]:
    path = os.environ.get("IMAGE_HASH_BLOCKLIST", "")
    if not path or not os.path.isfile(path):
        return []
    out = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#"):
                try:
                    out.append(int(line, 16))
                except ValueError:
                    continue
    return out


def _invalid(reason: str) -> ImageVerdict:
    return ImageVerdict(safe=False, action="block", reason=reason, categories=["invalid"])


def check_image(data: bytes) -> ImageVerdict:
    if not data:
        return _invalid("Empty file.")
    if len(data) > MAX_BYTES:
        return _invalid("File is too large.")
    if not (data[:3] == b"\xff\xd8\xff" or data[:8] == b"\x89PNG\r\n\x1a\n"):
        return _invalid("Only JPEG and PNG are accepted.")
    try:
        with Image.open(io.BytesIO(data)) as probe:
            probe.verify()  # structural integrity
        img = Image.open(io.BytesIO(data))
        img.load()
    except (UnidentifiedImageError, OSError, SyntaxError, Image.DecompressionBombError, ValueError):
        return _invalid("The image could not be read.")

    modelled = _classifier is not None
    h = average_hash(img)
    if any(_hamming(h, bad) <= HAMMING_THRESHOLD for bad in _blocklist()):
        return ImageVerdict(safe=False, action="block", reason="This image is not allowed.",
                            categories=["known_bad"], screened_by_model=modelled)

    if _classifier is None:
        return ImageVerdict(safe=True, action="allow", reason="No content model configured.",
                            categories=[], screened_by_model=False)

    scores = _classifier(img)
    nud, vio = scores.get("nudity", 0.0), scores.get("violence", 0.0)
    cats = [c for c, v in (("nudity", nud), ("violence", vio)) if v >= (0.5 if c == "nudity" else VIOLENCE_REVIEW)]
    if nud >= 0.85 or vio >= VIOLENCE_BLOCK:
        return ImageVerdict(safe=False, action="block", reason="This image breaks our community rules.",
                            categories=cats, screened_by_model=True)
    if cats:
        return ImageVerdict(safe=False, action="review", reason="This image needs a quick review.",
                            categories=cats, screened_by_model=True)
    return ImageVerdict(safe=True, action="allow", reason="ok", categories=[], screened_by_model=True)
