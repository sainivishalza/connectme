"""Dev only: give the demo people (ops/dev/seed_demo_people.sql) a few generated
photos each, so the swipe deck, photo carousel and profile sheet can be seen with
photos. The pictures are abstract gradients with a monogram, not people.

Needs the API running on 127.0.0.1:8080 and the ai-service venv for Pillow:

    set -a; . .env; set +a
    ai-service/.venv/Scripts/python ops/dev/seed_demo_photos.py

It signs a short-lived access token for each demo user from JWT_SECRET (the same
thing the load-test tool does), so it only works against your own local stack.
"""
import base64
import hashlib
import hmac
import io
import json
import os
import subprocess
import sys
import time
import urllib.request
import uuid

from PIL import Image, ImageDraw, ImageFont

API = os.environ.get("API", "http://127.0.0.1:8080")
SECRET = os.environ["JWT_SECRET"].encode()
PALETTES = [
    ((255, 170, 120), (236, 72, 120)), ((120, 180, 255), (120, 90, 230)),
    ((130, 220, 180), (40, 130, 150)), ((255, 210, 120), (230, 120, 80)),
]


def b64(b: bytes) -> str:
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode()


def token(user_id: str) -> str:
    head = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    now = int(time.time())
    body = b64(json.dumps({"role": "user", "typ": "access", "iss": "connectme", "sub": user_id, "iat": now, "exp": now + 600}).encode())
    sig = b64(hmac.new(SECRET, f"{head}.{body}".encode(), hashlib.sha256).digest())
    return f"{head}.{body}.{sig}"


def picture(initial: str, palette: tuple, variant: int) -> bytes:
    w, h = 900, 1200
    a, b = palette
    img = Image.new("RGB", (w, h))
    px = img.load()
    for y in range(h):
        t = y / h
        for x in range(w):
            u = (x / w * 0.4 + t * 0.6 + variant * 0.12) % 1.0
            px[x, y] = tuple(int(a[i] * (1 - u) + b[i] * u) for i in range(3))
    d = ImageDraw.Draw(img)
    try:
        font = ImageFont.truetype("C:/Windows/Fonts/georgiab.ttf", 420)
    except OSError:
        font = ImageFont.load_default()
    d.text((w / 2, h / 2 - 60 + variant * 40), initial, fill=(255, 255, 255), font=font, anchor="mm")
    out = io.BytesIO()
    img.save(out, "JPEG", quality=88)
    return out.getvalue()


def upload(tok: str, data: bytes) -> int:
    boundary = uuid.uuid4().hex
    body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"photo\"; filename=\"p.jpg\"\r\nContent-Type: image/jpeg\r\n\r\n").encode() + data + f"\r\n--{boundary}--\r\n".encode()
    req = urllib.request.Request(f"{API}/users/me/photos", data=body, method="POST",
                                 headers={"Authorization": f"Bearer {tok}", "Content-Type": f"multipart/form-data; boundary={boundary}"})
    try:
        return urllib.request.urlopen(req, timeout=30).status
    except urllib.error.HTTPError as e:
        return e.code


def main() -> None:
    sql = "SELECT u.id::text, COALESCE(p.display_name,'?'), jsonb_array_length(p.photos) FROM users u JOIN profiles p ON p.user_id=u.id WHERE u.email LIKE 'demo-%@example.test' ORDER BY u.email"
    rows = subprocess.run(["docker", "exec", "dating-app-postgres-1", "psql", os.environ["POSTGRES_URL"], "-At", "-F", "|", "-c", sql],
                          capture_output=True, text=True, check=True).stdout.strip().splitlines()
    for i, line in enumerate(rows):
        uid, name, have = line.split("|")
        if int(have) >= 3:
            print(f"{name}: already has {have} photos")
            continue
        for v in range(3 - int(have)):
            code = upload(token(uid), picture((name or "?")[0].upper(), PALETTES[i % len(PALETTES)], v))
            print(f"{name}: photo {v + 1} -> {code}")


if __name__ == "__main__":
    sys.exit(main())
