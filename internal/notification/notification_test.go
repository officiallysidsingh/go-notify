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

func TestMessageJSONUsesWireFieldNames(t *testing.T) {
	msg := Message{
		NotificationID: 7, UserID: "u", Title: "T", Priority: "3", Message: "hi", Type: TypePush,
	}

	want := `{"notification_id":7,"user_id":"u","title":"T","priority":"3","message":"hi","type":"push"}`
	if got := string(msg.JSON()); got != want {
		t.Errorf("JSON() = %s, want %s", got, want)
	}
}
