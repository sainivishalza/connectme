package errtrack

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Recoverer turns a panic in a handler into a 500, logs it, and reports it
// with the route pattern only. It replaces chi's Recoverer.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
				panic(rec)
			}
			err, ok := rec.(error)
			if !ok {
				err = errors.New(fmt.Sprint(rec))
			}
			route := "unknown"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = r.Method + " " + rc.RoutePattern()
			}
			slog.Error("panic in handler", "route", route, "err", Scrub(err.Error()), "reported", true)
			def.capture(fmt.Errorf("panic: %w", err), route, "fatal", 4)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal error"}`))
		}()
		next.ServeHTTP(w, r)
	})
}
