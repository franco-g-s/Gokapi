package models

// Download records one original file served to someone, through any route.
// UserId 0 is an anonymous download, stored as NULL.
type Download struct {
	FileId string
	UserId int
	At     int64
	Via    string
}

const (
	DownloadViaGroup      = "group"
	DownloadViaCollection = "collection"
	DownloadViaFile       = "file"
	DownloadViaZip        = "zip"
)

// CollectionSeen records the first time a signed-in person opened a
// collection link, for their "Shared with me" list
type CollectionSeen struct {
	UserId       int
	CollectionId string
	FirstOpened  int64
}
