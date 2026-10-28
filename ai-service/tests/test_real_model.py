"""Runs only when the exported model exists (python tools/export_nsfw_onnx.py)."""
import io
import os

import pytest
from fastapi.testclient import TestClient
from PIL import Image

MODEL = os.path.join(os.path.dirname(__file__), "..", "models", "nsfw.onnx")
pytest.importorskip("onnxruntime")
pytest.importorskip("av")
pytestmark = pytest.mark.skipif(not os.path.isfile(MODEL), reason="models/nsfw.onnx not exported")

from moderation import image_filter as imf  # noqa: E402
from moderation import onnx_classifier as oc  # noqa: E402
import main  # noqa: E402
from tests.test_video import clip  # noqa: E402


@pytest.fixture
def client(monkeypatch):
    monkeypatch.setenv("AI_SERVICE_KEY", "k" * 32)
    monkeypatch.setenv("NSFW_MODEL_PATH", MODEL)
    imf.set_classifier(oc.load_from_env())
    yield TestClient(main.app)
    imf.set_classifier(None)


def png(color):
    b = io.BytesIO()
    Image.new("RGB", (320, 240), color).save(b, "PNG")
    return b.getvalue()


def test_plain_images_pass_and_say_a_model_looked(client):
    r = client.post("/moderate/image", content=png((40, 90, 160)), headers={"X-Internal-Key": "k" * 32})
    assert r.status_code == 200
    j = r.json()
    assert j["action"] == "allow" and j["screened_by_model"] is True


def test_plain_video_passes_through_the_real_model(client, tmp_path):
    r = client.post("/moderate/video", content=clip(tmp_path, [(40, 90, 160), (200, 200, 60)]), headers={"Authorization": "Bearer " + "k" * 32})
    assert r.status_code == 200 and r.json()["action"] == "allow"


VIOLENCE = os.path.join(os.path.dirname(__file__), "..", "models", "violence.onnx")


@pytest.mark.skipif(not os.path.isfile(VIOLENCE), reason="models/violence.onnx not exported")
def test_both_real_models_load_together_and_plain_images_pass(monkeypatch):
    monkeypatch.setenv("NSFW_MODEL_PATH", MODEL)
    monkeypatch.setenv("VIOLENCE_MODEL_PATH", VIOLENCE)
    imf.set_classifier(oc.load_from_env())
    try:
        s = oc.load_from_env()(Image.new("RGB", (320, 240), (40, 90, 160)))
        assert set(s) == {"nudity", "violence"} and all(0.0 <= v <= 1.0 for v in s.values())
        assert imf.check_image(png((40, 90, 160))).screened_by_model is True
    finally:
        imf.set_classifier(None)
