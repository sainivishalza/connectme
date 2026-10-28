package games

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /games.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(2<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "games:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("read", 120, time.Minute)).Get("/prompt", h.prompt)
	r.With(lim("read", 60, time.Minute)).Get("/quiz/sets", h.sets)
	r.With(lim("quiz", 20, time.Hour)).Post("/quiz", h.start)
	r.With(lim("read", 90, time.Minute)).Get("/quiz/match/{id}", h.current)
	r.With(lim("answer", 400, time.Hour)).Post("/quiz/{id}/answer", h.answer)

	// Turn-based truth or dare.
	r.With(lim("read", 180, time.Minute)).Get("/td/{id}", h.tdState)
	r.With(lim("play", 60, time.Hour)).Post("/td/{id}/start", h.tdStart)
	r.With(lim("play", 600, time.Hour)).Post("/td/{id}/pick", h.tdPick)
	r.With(lim("play", 600, time.Hour)).Post("/td/{id}/done", h.tdDone)
	r.With(lim("play", 600, time.Hour)).Post("/td/{id}/skip", h.tdSkip)
	r.With(lim("play", 120, time.Hour)).Post("/td/{id}/take-over", h.tdTakeOver)
	r.With(lim("play", 60, time.Hour)).Delete("/td/{id}", h.tdEnd)

	// Group "would you rather" for a matched group chat (the id is the group match).
	r.With(lim("read", 180, time.Minute)).Get("/group/{id}", h.wyrState)
	r.With(lim("wyr-start", 60, time.Hour)).Post("/group/{id}/start", h.wyrStart)
	r.With(lim("wyr-vote", 300, time.Hour)).Post("/group/{id}/vote", h.wyrVote)
	r.With(lim("read", 180, time.Minute)).Get("/group/{id}/mlt", h.mltState)

	// Truth or dare for a matched group chat (the id is the group match) and in masked rooms (the id is the room).
	r.With(lim("read", 180, time.Minute)).Get("/party/{id}", h.partyState)
	r.With(lim("play", 60, time.Hour)).Post("/party/{id}/start", h.partyStart)
	r.With(lim("play", 600, time.Hour)).Post("/party/{id}/pick", h.partyPick)
	r.With(lim("play", 600, time.Hour)).Post("/party/{id}/done", h.partyDone)
	r.With(lim("play", 600, time.Hour)).Post("/party/{id}/skip", h.partySkip)
	r.With(lim("play", 120, time.Hour)).Post("/party/{id}/take-over", h.partyTakeOver)
	r.With(lim("play", 60, time.Hour)).Delete("/party/{id}", h.partyEnd)

	// Masked rooms: a queue of strangers under made-up names.
	r.With(lim("masked-join", 30, time.Hour)).Post("/masked/join", h.maskedJoin)
	r.With(lim("read", 240, time.Minute)).Get("/masked/status", h.maskedStatus)
	r.With(lim("play", 60, time.Hour)).Post("/masked/leave", h.maskedLeave)
	r.With(lim("read", 240, time.Minute)).Get("/masked/rooms/{id}", h.maskedRoom)
	r.With(lim("play", 600, time.Hour)).Post("/masked/rooms/{id}/finish", h.maskedFinish)
	r.With(lim("report", 30, time.Hour)).Post("/masked/rooms/{id}/answers/{aid}/report", h.maskedReport)
	r.With(lim("wyr-start", 60, time.Hour)).Post("/group/{id}/mlt/start", h.mltStart)
	r.With(lim("wyr-vote", 300, time.Hour)).Post("/group/{id}/mlt/vote", h.mltVote)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrCooldown):
		middleware.WriteError(w, http.StatusTooManyRequests, err.Error())
	default:
		slog.Error("games: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) prompt(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Prompt(r.URL.Query().Get("kind"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, p)
}

func (h *Handler) sets(w http.ResponseWriter, _ *http.Request) {
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"sets": h.svc.Sets()})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID string `json:"match_id"`
		SetID   string `json:"set_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	q, err := h.svc.Start(r.Context(), uid(r), req.MatchID, req.SetID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, q)
}

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	q, err := h.svc.Current(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"quiz": q})
}

func (h *Handler) answer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question int `json:"question"`
		Choice   int `json:"choice"`
	}
	if !decode(w, r, &req) {
		return
	}
	q, err := h.svc.Answer(r.Context(), uid(r), chi.URLParam(r, "id"), req.Question, req.Choice)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, q)
}

func (h *Handler) tdState(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.TDState(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) wyrState(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.WYRState(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) wyrStart(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.StartWYR(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) wyrVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PollID string `json:"poll_id"`
		Choice string `json:"choice"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := h.svc.VoteWYR(r.Context(), uid(r), chi.URLParam(r, "id"), req.PollID, req.Choice)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) tdStart(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.TDStart(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) tdPick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := h.svc.TDPick(r.Context(), uid(r), chi.URLParam(r, "id"), req.Kind)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) tdDone(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.TDFinish(r.Context(), uid(r), chi.URLParam(r, "id"), false)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) tdSkip(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.TDFinish(r.Context(), uid(r), chi.URLParam(r, "id"), true)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) tdTakeOver(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.TDTakeOver(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) tdEnd(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.TDEnd(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) mltState(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.MLTState(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) mltStart(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.StartMLT(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) mltVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PollID    string `json:"poll_id"`
		Candidate string `json:"candidate"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := h.svc.VoteMLT(r.Context(), uid(r), chi.URLParam(r, "id"), req.PollID, req.Candidate)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) partyJSON(w http.ResponseWriter, st *PartyTD, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) partyState(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.PartyState(r.Context(), uid(r), chi.URLParam(r, "id"))
	h.partyJSON(w, st, err)
}

func (h *Handler) partyStart(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.PartyStart(r.Context(), uid(r), chi.URLParam(r, "id"))
	h.partyJSON(w, st, err)
}

func (h *Handler) partyPick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := h.svc.PartyPick(r.Context(), uid(r), chi.URLParam(r, "id"), req.Kind)
	h.partyJSON(w, st, err)
}

func (h *Handler) partyDone(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.PartyFinish(r.Context(), uid(r), chi.URLParam(r, "id"), false)
	h.partyJSON(w, st, err)
}

func (h *Handler) partySkip(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.PartyFinish(r.Context(), uid(r), chi.URLParam(r, "id"), true)
	h.partyJSON(w, st, err)
}

func (h *Handler) partyTakeOver(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.PartyTakeOver(r.Context(), uid(r), chi.URLParam(r, "id"))
	h.partyJSON(w, st, err)
}

func (h *Handler) partyEnd(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.PartyEnd(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) maskedJoin(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.MaskedJoin(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) maskedStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.MaskedStatus(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) maskedLeave(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.MaskedLeave(r.Context(), uid(r)); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) maskedRoom(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.MaskedRoomState(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) maskedFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
		Skip bool   `json:"skip"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := h.svc.MaskedFinish(r.Context(), uid(r), chi.URLParam(r, "id"), req.Text, req.Skip)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) maskedReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.MaskedReport(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "aid"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
