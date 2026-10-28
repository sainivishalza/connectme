import io

import pytest
from PIL import Image

from moderation import image_filter as imf
from moderation import onnx_classifier as oc


class _Input:
    name = "pixel_values"


class _Out(list):
    pass


class FakeSession:
    """Stands in for onnxruntime: returns fixed logits and records the input shape."""

    def __init__(self, logits):
        self.logits = logits
        self.shape = None

    def get_inputs(self):
        return [_Input()]

    def run(self, _names, feed):
        self.shape = feed["pixel_values"].shape
        return [[self.logits]]


def png() -> bytes:
    b = io.BytesIO()
    Image.new("RGB", (64, 48), (200, 120, 90)).save(b, "PNG")
    return b.getvalue()


@pytest.fixture(autouse=True)
def _reset():
    yield
    imf.set_classifier(None)


def test_scores_are_a_softmax_over_the_nsfw_index():
    np = pytest.importorskip("numpy")
    del np
    sess = FakeSession([0.0, 4.0])
    c = oc.OnnxNudityClassifier(sess)
    s = c(Image.new("RGB", (100, 80)))
    assert 0.98 < s["nudity"] < 1.0
    assert sess.shape == (1, 3, 224, 224)
    assert oc.OnnxNudityClassifier(FakeSession([4.0, 0.0]))(Image.new("RGB", (10, 10)))["nudity"] < 0.02


def test_label_order_is_respected():
    pytest.importorskip("numpy")
    c = oc.OnnxNudityClassifier(FakeSession([5.0, 0.0]), labels=("nsfw", "normal"))
    assert c(Image.new("RGB", (10, 10)))["nudity"] > 0.98


def test_labels_must_name_nsfw():
    with pytest.raises(ValueError):
        oc.OnnxNudityClassifier(FakeSession([0, 1]), labels=("a", "b"))


def test_a_model_drives_the_verdict_end_to_end():
    pytest.importorskip("numpy")
    imf.set_classifier(oc.OnnxNudityClassifier(FakeSession([0.0, 6.0])))
    v = imf.check_image(png())
    assert (v.action, v.screened_by_model) == ("block", True) and "nudity" in v.categories
    imf.set_classifier(oc.OnnxNudityClassifier(FakeSession([6.0, 0.0])))
    v = imf.check_image(png())
    assert (v.action, v.screened_by_model) == ("allow", True)


def test_unset_path_means_no_model_and_a_bad_path_stops_startup(monkeypatch, tmp_path):
    monkeypatch.delenv("NSFW_MODEL_PATH", raising=False)
    assert oc.load_from_env() is None
    monkeypatch.setenv("NSFW_MODEL_PATH", str(tmp_path / "missing.onnx"))
    with pytest.raises(RuntimeError):
        oc.load_from_env()


def test_two_models_are_combined_and_violence_only_ever_goes_to_review(monkeypatch):
    pytest.importorskip("numpy")
    nude = oc.OnnxBinaryClassifier(FakeSession([6.0, 0.0]))  # normal
    viol = oc.OnnxBinaryClassifier(FakeSession([0.0, 6.0]), category="violence", positive="violence", labels=("nonviolence", "violence"))
    both = oc.Combined([nude, viol])
    s = both(Image.new("RGB", (10, 10)))
    assert s["nudity"] < 0.02 and s["violence"] > 0.98
    imf.set_classifier(both)
    v = imf.check_image(png())
    assert v.action == "review" and v.categories == ["violence"], "a violence score must not block by default"
    monkeypatch.setattr(imf, "VIOLENCE_BLOCK", 0.9)
    assert imf.check_image(png()).action == "block"
    monkeypatch.setattr(imf, "VIOLENCE_BLOCK", 1.01)
    monkeypatch.setattr(imf, "VIOLENCE_REVIEW", 0.99999)
    assert imf.check_image(png()).action in ("allow", "review")


def test_violence_path_alone_and_missing_file(monkeypatch, tmp_path):
    monkeypatch.delenv("NSFW_MODEL_PATH", raising=False)
    monkeypatch.setenv("VIOLENCE_MODEL_PATH", str(tmp_path / "missing.onnx"))
    with pytest.raises(RuntimeError):
        oc.load_from_env()
