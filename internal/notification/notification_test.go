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
