package models

// Collection groups several uploaded files behind a single public link.
//
// A collection is deliberately just a *view* over its files: it has no expiry,
// download budget or counter of its own. It lives for as long as at least one
// member still lives, and downloading through it behaves exactly as if the
// file had been downloaded from its own link. That keeps one clock instead of
// two, so a share can never be alive with nothing in it, or dead while its
// files are still fine.
type Collection struct {
	Id           string   `json:"id" redis:"id"`
	Name         string   `json:"name" redis:"name"`
	UserId       int      `json:"userid" redis:"userid"`
	CreationDate int64    `json:"creationdate" redis:"creationdate"`
	PasswordHash string   `json:"-" redis:"passwordhash"`
	FileIds      []string `json:"fileids" redis:"-"` // members, from the join table

	// Resolved by storage.GetCollection, which filters out expired members.
	Files          []File `json:"-" redis:"-"`
	TotalSizeBytes int64  `json:"totalsizebytes" redis:"-"`
}

// IsPasswordProtected returns true if a password is required to open the collection
func (c *Collection) IsPasswordProtected() bool {
	return c.PasswordHash != ""
}

// LatestExpiry returns the expiry of the longest-lived member, and whether any
// member never expires. This is the collection's effective expiry.
func (c *Collection) LatestExpiry() (int64, bool) {
	var latest int64
	for _, file := range c.Files {
		if file.UnlimitedTime {
			return 0, true
		}
		if file.ExpireAt > latest {
			latest = file.ExpireAt
		}
	}
	return latest, false
}
