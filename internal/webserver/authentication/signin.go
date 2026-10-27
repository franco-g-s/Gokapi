package authentication

import (
	"encoding/json"
	"errors"

	"github.com/forceu/gokapi/internal/models"
)

// ErrSignInRefused is returned by a guard that refuses a sign-in. The person
// sees the same "not authorised" page as any refused OAuth user, so a
// blocklist is never revealed.
var ErrSignInRefused = errors.New("sign-in refused")

// SignInGuard may refuse a sign-in before any account is created
type SignInGuard func(email string) error

// SignInHook runs after a successful OAuth sign-in, once the session exists.
// It must return quickly: slow work (a photo fetch) goes in a goroutine.
type SignInHook func(user models.User, claims map[string]any, created bool)

var signInGuards []SignInGuard
var signInHooks []SignInHook

// AddSignInGuard registers a guard. Only webserver.wireSignIn calls it.
func AddSignInGuard(g SignInGuard) { signInGuards = append(signInGuards, g) }

// AddSignInHook registers a hook. Only webserver.wireSignIn calls it.
func AddSignInHook(h SignInHook) { signInHooks = append(signInHooks, h) }

// ResetSignInForTest empties both lists
func ResetSignInForTest() { signInGuards, signInHooks = nil, nil }

// GuardCountForTest and HookCountForTest let the wiring test prove that
// wireSignIn registered everything: a dropped line fails a test, not a ban
func GuardCountForTest() int { return len(signInGuards) }

// HookCountForTest returns the number of registered hooks
func HookCountForTest() int { return len(signInHooks) }

func runSignInGuards(email string) error {
	for _, g := range signInGuards {
		if err := g(email); err != nil {
			return err
		}
	}
	return nil
}

func claimsMap(c OAuthUserClaims) map[string]any {
	result := map[string]any{}
	if c == nil {
		return result
	}
	var raw json.RawMessage
	if c.Claims(&raw) != nil || json.Unmarshal(raw, &result) != nil {
		return map[string]any{}
	}
	return result
}

func runSignInHooks(user models.User, claims OAuthUserClaims, created bool) {
	m := claimsMap(claims)
	for _, h := range signInHooks {
		h(user, m, created)
	}
}
