"""Request/response models. Inputs are deliberately minimal: no names, no
coordinates, no chat content. Everything is bounded so a caller cannot
make the service do unbounded work."""
from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field, field_validator

Tag = str


def _clean_tags(v: list[str]) -> list[str]:
    out, seen = [], set()
    for t in v:
        t = t.strip().lower()
        if t and t not in seen:
            seen.add(t)
            out.append(t[:40])
    return out[:20]


class Person(BaseModel):
    tags: list[Tag] = Field(default_factory=list, max_length=50)
    age: int | None = Field(default=None, ge=18, le=120)
    # Hours of day (0-23, UTC) in which the person is typically active.
    active_hours: list[int] = Field(default_factory=list, max_length=24)

    @field_validator("tags")
    @classmethod
    def _tags(cls, v: list[str]) -> list[str]:
        return _clean_tags(v)

    @field_validator("active_hours")
    @classmethod
    def _hours(cls, v: list[int]) -> list[int]:
        return sorted({h for h in v if 0 <= h <= 23})


class History(BaseModel):
    """Conversation *metadata* only (chats are end-to-end encrypted)."""

    a_to_b: int = Field(default=0, ge=0, le=100_000)
    b_to_a: int = Field(default=0, ge=0, le=100_000)


class ScoreRequest(BaseModel):
    a: Person
    b: Person
    history: History = Field(default_factory=History)


class Reason(BaseModel):
    code: Literal["shared_interests", "similar_age", "similar_schedule", "active_conversation"]
    text: str
    weight: float


class ScoreResponse(BaseModel):
    score: int = Field(ge=0, le=100)
    reasons: list[Reason]
    components: dict[str, float]


TextKind = Literal["bio", "intro", "name", "mood", "post", "group", "report", "message"]


class TextRequest(BaseModel):
    text: str = Field(max_length=4000)
    kind: TextKind = "bio"


class Verdict(BaseModel):
    safe: bool
    action: Literal["allow", "review", "block"]
    reason: str
    categories: list[str] = Field(default_factory=list)


class ImageVerdict(Verdict):
    # False when no nudity/violence model is configured: the image passed only
    # the structural and known-bad-hash checks.
    screened_by_model: bool = False


class SuggestRequest(BaseModel):
    activity: str = Field(default="custom", max_length=30)
    group_size: int = Field(default=2, ge=1, le=12)
    shared_tags: list[Tag] = Field(default_factory=list, max_length=20)

    @field_validator("shared_tags")
    @classmethod
    def _tags(cls, v: list[str]) -> list[str]:
        return _clean_tags(v)


class SuggestResponse(BaseModel):
    venues: list[str]
    icebreakers: list[str]
    source: str = "ai"
