"""ConnectMe AI service: compatibility scoring, moderation, wingman.

Internal service. Every route except /healthz requires the shared key in
X-Internal-Key; it must not be exposed to the internet. Payloads are never
logged, only outcomes.
"""
from __future__ import annotations

import hmac
import logging
import os

from fastapi.concurrency import run_in_threadpool
from fastapi import Depends, FastAPI, Header, HTTPException, Request, status

from matching.scorer import score
from moderation.content_filter import check_text
from moderation import onnx_classifier, video_filter
from moderation.image_filter import MAX_BYTES, check_image, set_classifier
from schemas import ImageVerdict, ReplyRequest, ReplyResponse, ScamCheckRequest, ScamCheckResponse, ScoreRequest, ScoreResponse, SuggestRequest, SuggestResponse, TextRequest, Verdict
from scam import detector as scam_detector
from wingman import llm as wingman_llm
from wingman.agent import REPLY_TEMPLATES, Wingman
from wingman.conversation_helper import suggest

log = logging.getLogger("ai-service")
app = FastAPI(title="ConnectMe AI", docs_url=None, redoc_url=None, openapi_url=None)

# With NSFW_MODEL_PATH set, images are scored by a local nudity model. A set
# but unloadable path stops startup rather than silently screening nothing.
set_classifier(onnx_classifier.load_from_env())


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


def require_bearer(authorization: str | None = Header(default=None)) -> None:
    """The Go API's HTTP video scanner authenticates with Authorization: Bearer
    <VIDEO_SCANNER_KEY>; accept the same shared key as the other routes."""
    token = authorization[7:] if authorization and authorization.lower().startswith("bearer ") else None
    require_key(token)


@app.post("/moderate/video", dependencies=[Depends(require_bearer)])
async def post_moderate_video(request: Request) -> dict[str, str]:
    """Contract of the API's HTTP video scanner: raw clip in, {action, reason} out."""
    limit = video_filter.MAX_BYTES + 1024
    declared = request.headers.get("content-length")
    if declared and declared.isdigit() and int(declared) > limit:
        raise HTTPException(413, "video too large")
    body = bytearray()
    async for chunk in request.stream():
        body.extend(chunk)
        if len(body) > limit:
            raise HTTPException(413, "video too large")
    try:
        action, reason = await run_in_threadpool(video_filter.check_video, bytes(body))
    except RuntimeError as e:
        log.error("video scan unavailable: %s", e)
        raise HTTPException(503, "no image model configured") from e  # the API holds the clip
    log.info("moderate video action=%s", action)
    return {"action": action, "reason": reason}


_wingman: Wingman | None = None
_wingman_ready = False


def get_wingman() -> Wingman | None:
    """The language-model wingman when WINGMAN_PROVIDER=llm and a key is set."""
    global _wingman, _wingman_ready
    if not _wingman_ready:
        model = wingman_llm.from_env()
        _wingman = Wingman(model) if model else None
        _wingman_ready = True
    return _wingman


@app.post("/wingman/suggest", response_model=SuggestResponse, dependencies=[Depends(require_key)])
def post_suggest(req: SuggestRequest) -> SuggestResponse:
    agent = get_wingman()
    if agent is not None:
        out = agent.suggest(req)
        if out is not None:
            log.info("wingman source=llm")
            return out
    return suggest(req)  # templates: always available, never fails


@app.post("/scam/check", response_model=ScamCheckResponse, dependencies=[Depends(require_key)])
def post_scam_check(req: ScamCheckRequest) -> ScamCheckResponse:
    """A second opinion on a few lines the member chose to share. Never stored or logged."""
    return scam_detector.check(req)


@app.post("/wingman/reply", response_model=ReplyResponse, dependencies=[Depends(require_key)])
def post_reply(req: ReplyRequest) -> ReplyResponse:
    """Reply ideas for a member who chose to share a few chat lines. Lines are
    never logged or stored here; with no model the answer is a template set."""
    agent = get_wingman()
    if agent is not None:
        out = agent.reply(req)
        if out is not None:
            log.info("wingman reply source=llm")
            return out
    start = sum(len(ln.text) for ln in req.lines) % len(REPLY_TEMPLATES)  # varied, not random: no state
    picks = [REPLY_TEMPLATES[(start + i) % len(REPLY_TEMPLATES)] for i in range(3)]
    return ReplyResponse(replies=picks, source="ai")
