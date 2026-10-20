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
