package notification

import "testing"

func TestIsSettledOnlyForSentAndFailed(t *testing.T) {
	cases := map[string]bool{
		StatusPending: false,
		StatusQueued:  false,
		StatusSent:    true,
		StatusFailed:  true,
		"unknown":     false,
	}
	for status, want := range cases {
		if got := IsSettled(status); got != want {
			t.Errorf("IsSettled(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestSupportedTypesIsPushOnly(t *testing.T) {
	got := SupportedTypes()
	if len(got) != 1 || got[0] != TypePush {
		t.Errorf("SupportedTypes() = %v, want [push]", got)
	}
}

func TestIsSupportedTypeAcceptsOnlyPush(t *testing.T) {
	cases := map[string]bool{TypePush: true, TypeEmail: false, TypeSMS: false, "": false}
	for typ, want := range cases {
		if got := IsSupportedType(typ); got != want {
			t.Errorf("IsSupportedType(%q) = %v, want %v", typ, got, want)
		}
	}
}
