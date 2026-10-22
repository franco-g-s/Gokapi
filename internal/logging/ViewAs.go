package logging

import (
	"fmt"

	"github.com/forceu/gokapi/internal/models"
)

// "View as…" (Part 3, V1) logs in the existing auth category, so the notifier
// emails every start and end at once: one Franco did not trigger is a
// break-in signal. Both calls block, so the line is written before the
// request that caused it returns.

// LogViewAsStart adds a log entry when an admin starts viewing as someone. Blocking
func LogViewAsStart(admin, target models.User) {
	createLogEntry(categoryAuth, fmt.Sprintf("%s (user #%d) started viewing as %s (user #%d)",
		admin.Name, admin.Id, target.Name, target.Id), true)
}

// LogViewAsEnd adds a log entry when an admin stops viewing as someone. Blocking.
// reason is one of exit, logout, timeout, switched, restart.
func LogViewAsEnd(admin, target models.User, reason string) {
	createLogEntry(categoryAuth, fmt.Sprintf("%s (user #%d) stopped viewing as %s (user #%d): %s",
		admin.Name, admin.Id, target.Name, target.Id, reason), true)
}
