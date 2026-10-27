//go:build test

package models

import (
	"testing"

	"github.com/forceu/gokapi/internal/test"
)

func TestPreviewVersionChangesWithUpdated(t *testing.T) {
	a := Preview{Sha1: "0123456789abcdef0123", Updated: 1000}
	b := Preview{Sha1: "0123456789abcdef0123", Updated: 1001}
	test.IsEqualBool(t, a.Version() != b.Version(), true)
	test.IsEqualString(t, a.Version(), Preview{Sha1: "0123456789abcdef0123", Updated: 1000}.Version())
	test.IsEqualString(t, a.Version(), "0123456789ab-rs")
	// short hashes (tests) are used whole
	test.IsEqualString(t, Preview{Sha1: "abc", Updated: 36}.Version(), "abc-10")
}
