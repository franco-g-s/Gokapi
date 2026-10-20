package authentication

// Return-after-sign-in for invite links (Part 2, G6): /join remembers itself
// in a short-lived cookie, and every sign-in success redirect goes there
// instead of "admin".

import (
	"net/http"
	"regexp"
)

const cookieReturnTo = "fgs_return"

// returnToPattern is the only target ever followed: the join page with a token
var returnToPattern = regexp.MustCompile(`^join\?t=[A-Za-z0-9_-]{1,64}$`)

// SetReturnTo remembers where to go after signing in. Only the join page is
// allowed, so this can never become an open redirect.
func SetReturnTo(w http.ResponseWriter, target string) {
	if !returnToPattern.MatchString(target) {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieReturnTo,
		Value:    target,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// PopReturnTo returns the remembered target, clearing it, or "admin". The
// cookie is client-controlled, so its value is checked again here.
func PopReturnTo(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(cookieReturnTo)
	if err != nil {
		return "admin"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieReturnTo,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	if !returnToPattern.MatchString(c.Value) {
		return "admin"
	}
	return c.Value
}
