package ratelimiter

// Joining a group by code or link (Part 2, G6). A code is six characters of
// a 30-letter alphabet, so guessing must be slow.

import (
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

var groupJoinLimiter = newLimiter()

// IsAllowedGroupJoin is reject-style, like IsAllowedNewUuid: 10 attempts,
// then one every 6 s, per IP and per account. Both must allow, and both are
// charged on every attempt.
func IsAllowedGroupJoin(ip string, userId int) bool {
	if isUnitTest {
		return true
	}
	byIp := groupJoinLimiter.Get("ip:"+ip, rate.Every(6*time.Second), 10).Allow()
	byUser := groupJoinLimiter.Get("user:"+strconv.Itoa(userId), rate.Every(6*time.Second), 10).Allow()
	return byIp && byUser
}
