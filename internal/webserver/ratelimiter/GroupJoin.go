package ratelimiter

// Joining a group by code or link (Part 2, G6). A code is six characters of
// a 30-letter alphabet, so guessing must be slow.

import (
	"net"
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
	byIp := groupJoinLimiter.Get("ip:"+joinIpKey(ip), rate.Every(6*time.Second), 10).Allow()
	byUser := groupJoinLimiter.Get("user:"+strconv.Itoa(userId), rate.Every(6*time.Second), 10).Allow()
	return byIp && byUser
}

// joinIpKey is the limiter's key for an address: an IPv6 address counts as
// its /64, since one host commonly holds a whole /64 (M9). IPv4 and anything
// unparsable are used as they are.
func joinIpKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
