package models

// Profile is what a person sets about themselves (Part 4, S1): the name
// shown to others, the name Google gave at the last sign-in, their Google
// photo and two switches. A missing row means every field's zero value.
type Profile struct {
	UserId         int
	DisplayName    string
	GoogleName     string
	Photo          []byte
	PhotoType      string
	PhotoUpdated   int64
	// PhotoTried is the last fetch attempt, successful or not: a picture
	// that keeps failing is still fetched at most once a day
	PhotoTried     int64
	UseInitials    bool
	EmailDownloads bool
}

// HasPhoto reports whether a photo is stored
func (p Profile) HasPhoto() bool { return len(p.Photo) > 0 && p.PhotoType != "" }
