import io

import pytest
from fastapi.testclient import TestClient
from PIL import Image

import moderation.image_filter as imf
from main import app
from wingman.conversation_helper import suggest
from schemas import SuggestRequest

H = {"X-Internal-Key": "test-key"}
client = TestClient(app)


def png(color=(120, 30, 200), size=(64, 64)) -> bytes:
    b = io.BytesIO()
    Image.new("RGB", size, color).save(b, "PNG")
    return b.getvalue()


def jpeg(size=(64, 64)) -> bytes:
    b = io.BytesIO()
    img = Image.new("RGB", size)
    img.putdata([(x * 4 % 256, y * 4 % 256, 99) for y in range(size[1]) for x in range(size[0])])
    img.save(b, "JPEG")
    return b.getvalue()


@pytest.fixture(autouse=True)
def _reset():
    imf.set_classifier(None)
    yield
    imf.set_classifier(None)


def test_valid_images_pass_and_admit_they_were_not_model_screened():
    for data in (png(), jpeg()):
        v = imf.check_image(data)
        assert v.action == "allow"
        assert v.screened_by_model is False


@pytest.mark.parametrize("data,why", [
    (b"", "empty"),
    (b"GIF89a....", "wrong type"),
    (b"<?php system($_GET['c']); ?>", "script"),
    (b"\xff\xd8\xff\xe0 not a real jpeg", "truncated"),
    (b"\x89PNG\r\n\x1a\n" + b"\x00" * 20, "corrupt png"),
])
def test_invalid_files_are_blocked(data, why):
    v = imf.check_image(data)
    assert v.action == "block", why
    assert v.categories == ["invalid"]


def test_oversize_is_blocked():
    assert imf.check_image(b"\xff\xd8\xff" + b"0" * (imf.MAX_BYTES + 1)).action == "block"


def test_known_bad_hash_is_blocked_even_after_resize(tmp_path, monkeypatch):
    img = Image.new("L", (256, 256))
    img.putdata([(x // 8 * 8) % 256 for y in range(256) for x in range(256)])
    buf = io.BytesIO()
    img.save(buf, "PNG")
    bad = imf.average_hash(Image.open(io.BytesIO(buf.getvalue())))
    f = tmp_path / "hashes.txt"
    f.write_text(f"# operator list\n{bad:016x}\n", encoding="utf-8")
    monkeypatch.setenv("IMAGE_HASH_BLOCKLIST", str(f))

    assert imf.check_image(buf.getvalue()).categories == ["known_bad"]
    small = io.BytesIO()
    img.resize((90, 90)).save(small, "JPEG")
    assert imf.check_image(small.getvalue()).action == "block", "perceptual match must survive resizing"
    assert imf.check_image(png((255, 255, 255))).action == "allow"


def test_pluggable_classifier_thresholds():
    imf.set_classifier(lambda im: {"nudity": 0.95})
    assert imf.check_image(png()).action == "block"
    imf.set_classifier(lambda im: {"nudity": 0.6})
    v = imf.check_image(png())
    assert v.action == "review" and v.screened_by_model is True
    imf.set_classifier(lambda im: {"nudity": 0.1, "violence": 0.0})
    assert imf.check_image(png()).action == "allow"


# ---- HTTP API -----------------------------------------------------------

def test_health_is_open_everything_else_needs_the_key():
    assert client.get("/healthz").json() == {"status": "ok"}
    for path, body in [("/score", {"a": {}, "b": {}}), ("/moderate", {"text": "hi"}), ("/wingman/suggest", {})]:
        assert client.post(path, json=body).status_code == 401
        assert client.post(path, json=body, headers={"X-Internal-Key": "wrong"}).status_code == 401
    assert client.post("/moderate/image", content=png()).status_code == 401


def test_service_refuses_to_run_open_without_a_key(monkeypatch):
    monkeypatch.delenv("AI_SERVICE_KEY")
    monkeypatch.delenv("AI_ALLOW_NO_KEY", raising=False)
    assert client.post("/moderate", json={"text": "hi"}, headers=H).status_code == 503
    monkeypatch.setenv("AI_ALLOW_NO_KEY", "1")
    assert client.post("/moderate", json={"text": "hi"}).status_code == 200


def test_score_endpoint():
    r = client.post("/score", headers=H, json={
        "a": {"tags": ["hiking", "coffee"], "age": 29, "active_hours": [18, 19, 20]},
        "b": {"tags": ["hiking", "jazz"], "age": 31, "active_hours": [19, 20, 21]},
        "history": {"a_to_b": 5, "b_to_a": 5},
    })
    assert r.status_code == 200
    j = r.json()
    assert 0 <= j["score"] <= 100
    assert j["reasons"] and j["reasons"][0]["code"] == "shared_interests"
    assert set(j["components"]) == {"interests", "age", "lifestyle", "schedule", "conversation"}


def test_inputs_are_validated_and_bounded():
    assert client.post("/score", headers=H, json={"a": {"age": 12}, "b": {}}).status_code == 422
    assert client.post("/moderate", headers=H, json={"text": "x" * 5000}).status_code == 422
    assert client.post("/moderate", headers=H, json={"text": "hi", "kind": "nonsense"}).status_code == 422
    assert client.post("/wingman/suggest", headers=H, json={"group_size": 999}).status_code == 422


def test_moderate_endpoint_and_image_endpoint():
    ok = client.post("/moderate", headers=H, json={"text": "love hiking", "kind": "bio"}).json()
    assert ok["action"] == "allow"
    bad = client.post("/moderate", headers=H, json={"text": "I'm 15", "kind": "bio"}).json()
    assert bad["action"] == "block" and bad["safe"] is False

    img = client.post("/moderate/image", headers=H, content=png())
    assert img.status_code == 200 and img.json()["action"] == "allow"
    assert client.post("/moderate/image", headers=H, content=b"not an image").json()["action"] == "block"
    too_big = client.post("/moderate/image", headers=H, content=b"\xff\xd8\xff" + b"0" * (imf.MAX_BYTES + 5000))
    assert too_big.status_code == 413


def test_wingman_is_deterministic_and_matches_the_backend_contract():
    body = {"activity": "hiking", "group_size": 4, "shared_tags": ["trail running", "coffee"]}
    a = client.post("/wingman/suggest", headers=H, json=body).json()
    b = client.post("/wingman/suggest", headers=H, json=body).json()
    assert a == b
    assert len(a["venues"]) == 3
    assert 5 <= len(a["icebreakers"]) <= 7
    assert any("trail running" in s for s in a["icebreakers"])
    assert a["source"] == "ai"
    assert suggest(SuggestRequest(activity="unknown-activity")).venues, "unknown activities fall back to custom ideas"
