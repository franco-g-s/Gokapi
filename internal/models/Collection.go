package models

import (
	"strconv"
	"time"
)

// Collection groups several uploaded files behind a single public link.
//
// A collection is many-to-many: a file can belong to more than one collection
// and always keeps its own standalone download link. The collection owns its
// own expiry, download limit and password, which govern access through the
// collection link only.
type Collection struct {
	Id                 string   `json:"id" redis:"id"`                                 // Public ID used in the share URL
	Name               string   `json:"name" redis:"name"`                             // Display name, e.g. "Holiday photos"
	UserId             int      `json:"userid" redis:"userid"`                         // Owner
	CreationDate       int64    `json:"creationdate" redis:"creationdate"`             // Unix timestamp of creation
	ExpireAt           int64    `json:"expireat" redis:"expireat"`                     // Unix timestamp of expiry
	UnlimitedTime      bool     `json:"unlimitedtime" redis:"unlimitedtime"`           // If true, ExpireAt is ignored
	DownloadsRemaining int      `json:"downloadsremaining" redis:"downloadsremaining"` // Downloads left before expiry
	UnlimitedDownloads bool     `json:"unlimiteddownloads" redis:"unlimiteddownloads"` // If true, DownloadsRemaining is ignored
	DownloadCount      int      `json:"downloadcount" redis:"downloadcount"`           // Total downloads served
	PasswordHash       string   `json:"-" redis:"passwordhash"`                        // Optional password, empty if unprotected
	FileIds            []string `json:"fileids" redis:"-"`                             // Member file IDs, loaded from the join table
	Files              []File   `json:"-" redis:"-"`                                   // Resolved members. Needs Populate()
	TotalSizeBytes     int64    `json:"totalsizebytes" redis:"-"`                      // Combined size. Needs Populate()
}

// Populate resolves FileIds into Files using the supplied metadata map.
//
// Members that no longer exist or are pending deletion are skipped rather than
// treated as an error: one dead file must never break the whole collection.
func (c *Collection) Populate(files map[string]File) {
	c.Files = make([]File, 0, len(c.FileIds))
	c.TotalSizeBytes = 0
	for _, id := range c.FileIds {
		file, ok := files[id]
		if !ok || file.IsPendingForDeletion() {
			continue
		}
		c.Files = append(c.Files, file)
		c.TotalSizeBytes = c.TotalSizeBytes + file.SizeBytes
	}
}

// IsExpired returns true if the collection can no longer be downloaded, either
// because its expiry date has passed or it has no downloads remaining.
func (c *Collection) IsExpired(timeNow int64) bool {
	return (!c.UnlimitedTime && c.ExpireAt < timeNow) ||
		(!c.UnlimitedDownloads && c.DownloadsRemaining < 1)
}

// IsPasswordProtected returns true if a password is required to open the collection
func (c *Collection) IsPasswordProtected() bool {
	return c.PasswordHash != ""
}

// ExpireAtString returns the expiry as a human readable string
func (c *Collection) ExpireAtString() string {
	if c.UnlimitedTime {
		return "Unlimited"
	}
	return time.Unix(c.ExpireAt, 0).Format("2006-01-02 15:04")
}

// DownloadsRemainingString returns the remaining downloads as a human readable string
func (c *Collection) DownloadsRemainingString() string {
	if c.UnlimitedDownloads {
		return "Unlimited"
	}
	return strconv.Itoa(c.DownloadsRemaining)
}
