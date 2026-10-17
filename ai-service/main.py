"""ConnectMe AI service: compatibility scoring, moderation, wingman.

Internal service. Every route except /healthz requires the shared key in
X-Internal-Key; it must not be exposed to the internet. Payloads are never
logged, only outcomes.
"""
from __future__ import annotations

import hmac
import logging
import os

from fastapi import Depends, FastAPI, Header, HTTPException, Request, status

from matching.scorer import score
from moderation.content_filter import check_text
from moderation.image_filter import MAX_BYTES, check_image
from schemas import ImageVerdict, ScoreRequest, ScoreResponse, SuggestRequest, SuggestResponse, TextRequest, Verdict
from wingman.conversation_helper import suggest

log = logging.getLogger("ai-service")
app = FastAPI(title="ConnectMe AI", docs_url=None, redoc_url=None, openapi_url=None)


def require_key(x_internal_key: str | None = Header(default=None)) -> None:
    """Constant-time shared-secret check. With no key configured the service
    refuses everything unless AI_ALLOW_NO_KEY=1 (local development only)."""
    expected = os.environ.get("AI_SERVICE_KEY", "")
    if not expected:
        if os.environ.get("AI_ALLOW_NO_KEY") == "1":
            return
        raise HTTPException(status.HTTP_503_SERVICE_UNAVAILABLE, "service not configured")
    if not x_internal_key or not hmac.compare_digest(x_internal_key, expected):
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "unauthorized")


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/score", response_model=ScoreResponse, dependencies=[Depends(require_key)])
def post_score(req: ScoreRequest) -> ScoreResponse:
    return score(req)


@app.post("/moderate", response_model=Verdict, dependencies=[Depends(require_key)])
def post_moderate(req: TextRequest) -> Verdict:
    v = check_text(req.text, req.kind)
    log.info("moderate kind=%s action=%s categories=%s", req.kind, v.action, v.categories)  # never the text
    return v


@app.post("/moderate/image", response_model=ImageVerdict, dependencies=[Depends(require_key)])
async def post_moderate_image(request: Request) -> ImageVerdict:
    limit = MAX_BYTES + 1024
    declared = request.headers.get("content-length")
    if declared and declared.isdigit() and int(declared) > limit:
        raise HTTPException(413, "image too large")
    body = bytearray()
    async for chunk in request.stream():
        body.extend(chunk)
        if len(body) > limit:
            raise HTTPException(413, "image too large")
    v = check_image(bytes(body))
    log.info("moderate image action=%s categories=%s model=%s", v.action, v.categories, v.screened_by_model)
    return v


@app.post("/wingman/suggest", response_model=SuggestResponse, dependencies=[Depends(require_key)])
def post_suggest(req: SuggestRequest) -> SuggestResponse:
    return suggest(req)
