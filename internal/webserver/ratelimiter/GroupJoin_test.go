package ratelimiter

import (
	"testing"

	"github.com/forceu/gokapi/internal/test"
)

func TestGroupJoinLimit(t *testing.T) {
	previous := isUnitTest
	SetUnitTestMode(false)
	defer SetUnitTestMode(previous)

	// Ten attempts for one ip and account are allowed, the eleventh is refused
	for i := 0; i < 10; i++ {
		test.IsEqualBool(t, IsAllowedGroupJoin("10.0.0.1", 101), true)
	}
	test.IsEqualBool(t, IsAllowedGroupJoin("10.0.0.1", 101), false)

	// Once an IP has used its burst, a different account from it is refused
	for i := 0; i < 10; i++ {
		test.IsEqualBool(t, IsAllowedGroupJoin("10.0.0.2", 201), true)
	}
	test.IsEqualBool(t, IsAllowedGroupJoin("10.0.0.2", 202), false)

	// and an account that used its burst is refused from a different IP
	for i := 0; i < 10; i++ {
		test.IsEqualBool(t, IsAllowedGroupJoin("10.0.0.3", 301), true)
	}
	test.IsEqualBool(t, IsAllowedGroupJoin("10.0.0.4", 301), false)
}

func TestGroupJoinUnitTestMode(t *testing.T) {
	previous := isUnitTest
	SetUnitTestMode(true)
	defer SetUnitTestMode(previous)
	for i := 0; i < 20; i++ {
		test.IsEqualBool(t, IsAllowedGroupJoin("10.0.0.9", 901), true)
	}
}

// M9: IPv6 addresses share a limit per /64
func TestJoinIpKey(t *testing.T) {
	test.IsEqualString(t, joinIpKey("2001:db8:1:2:aaaa::1"), "2001:db8:1:2::/64")
	test.IsEqualString(t, joinIpKey("2001:db8:1:2:ffff:ffff:ffff:ffff"), "2001:db8:1:2::/64")
	test.IsEqualBool(t, joinIpKey("2001:db8:1:3::1") != joinIpKey("2001:db8:1:2::1"), true)
	test.IsEqualString(t, joinIpKey("203.0.113.7"), "203.0.113.7")
	test.IsEqualString(t, joinIpKey("::ffff:203.0.113.7"), "::ffff:203.0.113.7")
	test.IsEqualString(t, joinIpKey("not an ip"), "not an ip")
}
