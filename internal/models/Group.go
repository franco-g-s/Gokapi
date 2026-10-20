package models

// Group is a shared space signed-in people upload into. It is unlisted: you
// learn of it only from its invite link or code (the admin sees all).
type Group struct {
	Id          string `json:"id" redis:"id"`
	Name        string `json:"name" redis:"name"`
	CreatorId   int    `json:"creatorId" redis:"creatorid"`
	Created     int64  `json:"created" redis:"created"`
	Expires     int64  `json:"expires" redis:"expires"` // 0 = never
	JoinCode    string `json:"-" redis:"joincode"`      // "K7P-4QX", unique; shown to members, reset with the token
	InviteToken string `json:"-" redis:"invitetoken"`   // the t= of /g/join; the code derives from it
}

// NeverExpires is true for a group created or extended with "Never"
func (g Group) NeverExpires() bool { return g.Expires == 0 }

// IsExpired reports whether the group has closed at now
func (g Group) IsExpired(now int64) bool { return g.Expires != 0 && g.Expires <= now }

const (
	GroupRoleCreator = "creator"
	GroupRoleMember  = "member"
)

// GroupMember is one person in a group
type GroupMember struct {
	GroupId string `json:"groupId"`
	UserId  int    `json:"userId"`
	Joined  int64  `json:"joined"`
	Role    string `json:"role"`
}
