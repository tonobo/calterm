package model

import "testing"

func TestEmailAddressNormalizesMailtoAndEscapes(t *testing.T) {
	if got := EmailAddress("MAILTO:User%2BWork@Example.COM"); got != "user+work@example.com" {
		t.Errorf("EmailAddress = %q", got)
	}
}

func TestHiddenSetMatchesQualifiedEntries(t *testing.T) {
	set := HiddenSet([]string{"personal/work", "work/personal"})

	if !IsHidden(set, "personal", "work") {
		t.Error("personal/work should be hidden")
	}
	if !IsHidden(set, "work", "personal") {
		t.Error("work/personal should be hidden")
	}
	// The two entries are independent: neither hides the other's mirror image.
	if IsHidden(set, "personal", "personal") {
		t.Error("personal/personal should NOT be hidden")
	}
	if IsHidden(set, "work", "work") {
		t.Error("work/work should NOT be hidden")
	}
}

// Two accounts each exposing a calendar with the same ID must be hideable
// independently -- the collision this keying exists to fix.
func TestHiddenSetDistinguishesSameCalendarIDAcrossAccounts(t *testing.T) {
	set := HiddenSet([]string{"work/personal"})

	if !IsHidden(set, "work", "personal") {
		t.Error("work/personal should be hidden")
	}
	if IsHidden(set, "home", "personal") {
		t.Error("home/personal must not be hidden by an entry for work/personal")
	}
}

// A bare calendar ID in an existing config keeps working, matching any account.
func TestHiddenSetAcceptsBareCalendarIDs(t *testing.T) {
	set := HiddenSet([]string{"birthdays"})

	if !IsHidden(set, "personal", "birthdays") {
		t.Error("a bare ID should match regardless of account")
	}
	if !IsHidden(set, "work", "birthdays") {
		t.Error("a bare ID should match every account")
	}
	if IsHidden(set, "personal", "work") {
		t.Error("a bare ID must not match a different calendar")
	}
}

func TestHiddenSetEmptyHidesNothing(t *testing.T) {
	for _, set := range []map[string]bool{HiddenSet(nil), HiddenSet([]string{})} {
		if IsHidden(set, "personal", "work") {
			t.Error("an empty set should hide nothing")
		}
	}
}

func TestIsHiddenOnANilSet(t *testing.T) {
	if IsHidden(nil, "personal", "work") {
		t.Error("a nil set should hide nothing rather than panicking")
	}
}
