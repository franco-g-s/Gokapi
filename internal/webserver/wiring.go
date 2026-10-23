package webserver

import (
	"github.com/forceu/gokapi/internal/activity"
	"github.com/forceu/gokapi/internal/blocklist"
	"github.com/forceu/gokapi/internal/people"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
)

// wireSignIn registers every sign-in guard and hook, in one place, so none
// can silently go missing (Part 4). Called once from Start.
func wireSignIn() {
	// Part 4, K: blocklist guard
	authentication.AddSignInGuard(blocklist.Guard)
	sessionmanager.SessionRefused = blocklist.SessionRefused
	// Part 4, A: first sign-in activity
	authentication.AddSignInHook(activity.OnSignIn)
	// Part 4, S1: profile capture
	authentication.AddSignInHook(people.OnSignIn)
}
