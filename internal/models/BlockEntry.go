package models

// BlockEntry is one line of the blocklist (Part 4, K): an exact email or an
// "@domain", always lower case
type BlockEntry struct {
	Entry   string
	Added   int64
	AddedBy int
}
