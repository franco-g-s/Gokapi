package models

// ViewAs is an admin looking at the site through another person's eyes
// (Part 3, V1). It is keyed to the admin's user id, not a session, so every
// session of that admin is in "View as…" at once.
type ViewAs struct {
	AdminId  int
	TargetId int
	Started  int64
	LastSeen int64
}
