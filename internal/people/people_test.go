//go:build test

package people

import (
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestNameFallsBackToEmailPrefix(t *testing.T) {
	test.IsEqualString(t, Name(models.User{Id: 1, Name: "anna.mueller@example.com"}), "anna.mueller")
	test.IsEqualString(t, Name(models.User{Id: 2, Name: "ben"}), "ben")
	test.IsEqualString(t, Name(models.User{Id: 3, Name: "@odd"}), "@odd")
}

func TestInitials(t *testing.T) {
	test.IsEqualString(t, InitialsOfName("anna.mueller"), "AM")
	test.IsEqualString(t, InitialsOfName("Ben"), "B")
	test.IsEqualString(t, Initials(models.User{Id: 1, Name: "carla.r@example.com"}), "CR")
}

func TestHasEmail(t *testing.T) {
	test.IsEqualBool(t, HasEmail(models.User{Name: "a@b.ch"}), true)
	test.IsEqualBool(t, HasEmail(models.User{Name: "tester"}), false)
}
