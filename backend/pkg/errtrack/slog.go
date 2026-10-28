package errtrack

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// Wrap returns a slog handler that behaves like next and also reports every
// ERROR-level record. Handlers log their unexpected failures at ERROR, so this
// covers 5xx paths that do not panic. Only the message and the "err" and
// "route" attributes are used, and the text is scrubbed before it leaves.
func Wrap(next slog.Handler) slog.Handler { return &logHandler{next: next} }

type logHandler struct{ next slog.Handler }

func (h *logHandler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h *logHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError && def != nil {
		route, detail, already := "log", "", false
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "reported":
				already = true
			case "route":
				route = a.Value.String()
			case "err":
				detail = a.Value.String()
			}
			return true
		})
		if already {
			return h.next.Handle(ctx, r)
		}
		msg := r.Message
		if detail != "" {
			msg += ": " + detail
		}
		def.capture(errors.New(strings.TrimSpace(msg)), route, "error", 5)
	}
	return h.next.Handle(ctx, r)
}

func (h *logHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &logHandler{next: h.next.WithAttrs(a)}
}
func (h *logHandler) WithGroup(n string) slog.Handler { return &logHandler{next: h.next.WithGroup(n)} }
