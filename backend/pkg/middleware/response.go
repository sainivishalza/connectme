package middleware

import (
	"encoding/json"
	"net/http"
)

// WriteError writes a JSON error body. Messages must be safe for clients:
// never pass internal error text here.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteModerationUnavailable answers 503 with Retry-After when content cannot
// be screened. The body code is what the app maps to a friendly message.
func WriteModerationUnavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	WriteError(w, http.StatusServiceUnavailable, "moderation_unavailable")
}

// DecodeStrict decodes a JSON request body into v and refuses unknown fields,
// so a client cannot send a field the struct does not declare (role, tier,
// coins, is_verified...). Use it, or a decoder with DisallowUnknownFields, for
// every request body.
func DecodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
