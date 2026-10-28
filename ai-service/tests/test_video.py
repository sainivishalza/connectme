import io
import os

import pytest
from fastapi.testclient import TestClient

av = pytest.importorskip("av")
np = pytest.importorskip("numpy")

from moderation import image_filter as imf  # noqa: E402
import main  # noqa: E402


def make_mp4(path, colors, fps=10, per=10):
    with av.open(path, "w") as c:
        s = c.add_stream("mpeg4", rate=fps)
        s.width, s.height, s.pix_fmt = 128, 96, "yuv420p"
        for col in colors:
            for _ in range(per):
                arr = np.zeros((96, 128, 3), dtype="uint8")
                arr[:, :] = col
                for p in s.encode(av.VideoFrame.from_ndarray(arr, format="rgb24")):
                    c.mux(p)
        for p in s.encode():
            c.mux(p)


def clip(tmp_path, colors):
    p = str(tmp_path / "c.mp4")
    make_mp4(p, colors)
    return open(p, "rb").read()


def red_is_nude(img):
    r, g, b = img.convert("RGB").resize((1, 1)).getpixel((0, 0))
    return {"nudity": 0.97 if r > 180 and g < 80 else (0.6 if r > 120 and g < 60 else 0.01)}


@pytest.fixture
def client(monkeypatch):
    monkeypatch.setenv("AI_SERVICE_KEY", "k" * 32)
    yield TestClient(main.app)
    imf.set_classifier(None)


def scan(client, data, key="k" * 32):
    return client.post("/moderate/video", content=data, headers={"Authorization": f"Bearer {key}"})


def test_clean_clip_allowed_and_one_bad_scene_blocks(client, tmp_path):
    imf.set_classifier(red_is_nude)
    ok = scan(client, clip(tmp_path, [(20, 160, 40), (30, 30, 200)]))
    assert ok.status_code == 200 and ok.json()["action"] == "allow"
    bad = scan(client, clip(tmp_path, [(20, 160, 40), (230, 20, 20), (30, 30, 200)]))
    assert bad.json()["action"] == "block"
    # The reason never describes content in detail.
    assert "rules" in bad.json()["reason"]


def test_borderline_frame_goes_to_review(client, tmp_path):
    imf.set_classifier(red_is_nude)
    r = scan(client, clip(tmp_path, [(150, 20, 20)] * 2))
    assert r.json()["action"] == "flag"


def test_garbage_is_blocked_not_allowed(client):
    imf.set_classifier(red_is_nude)
    r = scan(client, b"this is not a video" * 100)
    assert r.status_code == 200 and r.json()["action"] == "block"


def test_without_a_model_the_service_refuses_so_the_api_holds_the_clip(client, tmp_path):
    imf.set_classifier(None)
    assert scan(client, clip(tmp_path, [(20, 160, 40)])).status_code == 503


def test_video_route_needs_the_key(client, tmp_path):
    imf.set_classifier(red_is_nude)
    data = clip(tmp_path, [(20, 160, 40)])
    assert scan(client, data, key="wrong").status_code == 401
    assert client.post("/moderate/video", content=data).status_code in (401, 503)
