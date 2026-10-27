// Package downloadlog tags an HTTP request with where a download came from, so
// that storage can record one Downloads row per original file it serves
// without every serving function growing parameters.
package downloadlog

import (
	"context"
	"net/http"
)

// Source says through which route a request is served and for whom
type Source struct {
	Via    string // models.DownloadVia*
	UserId int    // 0 = anonymous
}

type ctxKey struct{}

// With tags the request: storage records one Downloads row per original file
// it serves for this request. Untagged requests record nothing.
func With(r *http.Request, via string, userId int) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, Source{Via: via, UserId: userId}))
}

// From returns the tag set by With, and false for an untagged request
func From(r *http.Request) (Source, bool) {
	s, ok := r.Context().Value(ctxKey{}).(Source)
	return s, ok
}
