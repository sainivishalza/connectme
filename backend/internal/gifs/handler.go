package gifs

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ p Provider }

// Routes mounts /gifs. With no provider the routes still exist and answer
// with an empty list, so the app can show a friendly "not available".
func Routes(p Provider, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{p: p}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(1<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "gifs:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("search", 60, time.Minute)).Get("/search", h.search)
	r.With(lim("media", 300, time.Minute)).Get("/{id}", h.media)
	return r
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	if h.p == nil {
		middleware.WriteJSON(w, http.StatusOK, map[string]any{"items": []Item{}, "provider": "none"})
		return
	}
	limit := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v >= 1 && v <= 30 {
		limit = v
	}
	items, err := h.p.Search(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		slog.Warn("gifs: search failed", "provider", h.p.Name(), "err", err)
		middleware.WriteError(w, http.StatusBadGateway, "GIF search is unavailable right now")
		return
	}
	if items == nil {
		items = []Item{}
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "provider": h.p.Name()})
}

func (h *Handler) media(w http.ResponseWriter, r *http.Request) {
	if h.p == nil {
		middleware.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	data, err := h.p.Media(r.Context(), chi.URLParam(r, "id"))
	switch {
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case err != nil:
		slog.Warn("gifs: media failed", "provider", h.p.Name(), "err", err)
		middleware.WriteError(w, http.StatusBadGateway, "GIF unavailable")
	default:
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(data)
	}
}
