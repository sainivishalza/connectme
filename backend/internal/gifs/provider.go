// Package gifs lets members find an animated image and send it in a chat.
//
// Why a server in the middle: chats are end-to-end encrypted, and a GIF
// service would otherwise learn who searches for what and from which address.
// The app asks this server, and this server asks the provider, so the provider
// sees only the server. The chosen GIF is then downloaded through us and sent
// as an ordinary encrypted attachment: the other person's app never contacts
// the provider at all.
//
// Providers: "builtin" (generated animated stickers, no account or key, works
// offline), "tenor" (needs TENOR_API_KEY), or none.
package gifs

import (
	"context"
	"errors"
	"strings"
)

// Item is one search result. Only the id and a title go to the app; the image
// itself is fetched from this server by id.
type Item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

var ErrNotFound = errors.New("not found")

// Provider finds and serves animated images.
type Provider interface {
	Name() string
	// Search returns up to limit results; an empty query returns popular ones.
	Search(ctx context.Context, query string, limit int) ([]Item, error)
	// Media returns the GIF bytes for an id returned by Search.
	Media(ctx context.Context, id string) ([]byte, error)
}

// CleanQuery trims, folds whitespace and caps the length of a search phrase.
func CleanQuery(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if r := []rune(q); len(r) > 40 {
		q = string(r[:40])
	}
	return q
}
