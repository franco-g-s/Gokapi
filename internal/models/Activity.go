package models

// Activity is one event in the admin's activity feed (Part 4). Rows are
// written when the event happens; the autoincrement Id is the feed cursor.
// ActorId 0 is "someone" (nobody signed in) or the system.
type Activity struct {
	Id        int64
	At        int64
	Kind      string
	ActorId   int
	OwnerId   int
	FileId    string
	FileName  string
	FileSize  int64
	FileKind  string
	GroupId   string
	GroupName string
	Via       string
	Detail    string
}

// Activity kinds, as they appear in the feed
const (
	ActivityUpload         = "upload"
	ActivityDownload       = "download"
	ActivityGroupCreated   = "group_created"
	ActivityGroupJoined    = "group_joined"
	ActivityFirstSignIn    = "first_signin"
	ActivitySignInBlocked  = "signin_blocked"
	ActivityWarningDisk    = "warning_disk"
	ActivityWarningPreview = "warning_preview"
	ActivityViewAsStart    = "viewas_start"
	ActivityViewAsEnd      = "viewas_end"
)
