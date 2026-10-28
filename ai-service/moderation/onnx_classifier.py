"""A local nudity classifier backed by an ONNX image-classification model.

Configure with NSFW_MODEL_PATH (an .onnx file). Defaults match the common
ViT-style "normal / nsfw" models (224x224, mean/std 0.5, logits in the order
given by NSFW_LABELS). Violence is not covered by this model, so it reports
only "nudity" score on its own; the pluggable-classifier contract in image_filter
treats a missing category as 0.

If a model path is set but the model cannot be loaded, startup fails: an
operator who asked for screening must never silently get none.
"""
from __future__ import annotations

import math
import os
from typing import Any

from PIL import Image


class OnnxBinaryClassifier:
    """A two-class ONNX image model reporting the probability of one class
    under a category name ("nudity" or "violence")."""

    def __init__(self, session: Any, *, category: str = "nudity", positive: str = "nsfw", size: int = 224,
                 mean: float = 0.5, std: float = 0.5, labels: tuple[str, ...] = ("normal", "nsfw")) -> None:
        if positive not in labels:
            raise ValueError(f"labels must include {positive!r}")
        self._session = session
        self._input = session.get_inputs()[0].name
        self._size, self._mean, self._std = size, mean, std
        self._idx = labels.index(positive)
        self._category = category

    def _tensor(self, img: Image.Image) -> list:
        im = img.convert("RGB").resize((self._size, self._size), Image.Resampling.BICUBIC)
        import numpy as np  # imported here so the module loads without numpy in unit tests

        a = np.asarray(im, dtype="float32") / 255.0
        a = (a - self._mean) / self._std
        return np.transpose(a, (2, 0, 1))[None, ...].astype("float32")

    def __call__(self, img: Image.Image) -> dict[str, float]:
        out = self._session.run(None, {self._input: self._tensor(img)})[0]
        logits = [float(x) for x in out[0]]
        m = max(logits)
        exps = [math.exp(x - m) for x in logits]
        return {self._category: exps[self._idx] / sum(exps)}


# The nudity model is the original use of this class.
OnnxNudityClassifier = OnnxBinaryClassifier


class Combined:
    """Runs several classifiers and merges their category scores."""

    def __init__(self, parts: list) -> None:
        self._parts = parts

    def __call__(self, img: Image.Image) -> dict[str, float]:
        out: dict[str, float] = {}
        for p in self._parts:
            out.update(p(img))
        return out


def _session(path: str):
    if not os.path.isfile(path):
        raise RuntimeError(f"model path does not exist: {path}")
    import onnxruntime as ort

    return ort.InferenceSession(path, providers=["CPUExecutionProvider"])


def _labels(var: str, default: str) -> tuple[str, ...]:
    return tuple(s.strip() for s in os.environ.get(var, default).split(",") if s.strip())


def load_from_env():
    """The configured classifier: nudity (NSFW_MODEL_PATH), violence
    (VIOLENCE_MODEL_PATH), both, or None. A path that is set but unusable
    stops startup."""
    parts = []
    nsfw = os.environ.get("NSFW_MODEL_PATH", "").strip()
    if nsfw:
        parts.append(OnnxBinaryClassifier(
            _session(nsfw), category="nudity", positive="nsfw",
            size=int(os.environ.get("NSFW_INPUT_SIZE", "224")), mean=float(os.environ.get("NSFW_MEAN", "0.5")),
            std=float(os.environ.get("NSFW_STD", "0.5")), labels=_labels("NSFW_LABELS", "normal,nsfw")))
    vio = os.environ.get("VIOLENCE_MODEL_PATH", "").strip()
    if vio:
        parts.append(OnnxBinaryClassifier(
            _session(vio), category="violence", positive="violence",
            size=int(os.environ.get("VIOLENCE_INPUT_SIZE", "224")), mean=float(os.environ.get("VIOLENCE_MEAN", "0.5")),
            std=float(os.environ.get("VIOLENCE_STD", "0.5")), labels=_labels("VIOLENCE_LABELS", "nonviolence,violence")))
    if not parts:
        return None
    return parts[0] if len(parts) == 1 else Combined(parts)
