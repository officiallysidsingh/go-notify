package notification

import "encoding/json"

const (
	TypeEmail = "email"
	TypeSMS   = "sms"
	TypePush  = "push"
)

const (
	StatusPending = "pending"
	StatusQueued  = "queued"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

// Message is the payload exchanged between the gRPC server and the worker.
type Message struct {
	NotificationID int64  `json:"notification_id"`
	UserID         string `json:"user_id"`
	Title          string `json:"title"`
	Priority       string `json:"priority"`
	Message        string `json:"message"`
	Type           string `json:"type"`
}

// JSON returns the wire encoding of the message. Message holds only strings
// and an integer, which encoding/json always marshals, so there is no error to
// report.
func (m Message) JSON() []byte {
	body, _ := json.Marshal(m)
	return body
}

func Types() []string {
	return []string{TypeEmail, TypeSMS, TypePush}
}

// SupportedTypes lists the types the API accepts. Types() is the wider set
// used for the queue layout.
func SupportedTypes() []string {
	return []string{TypePush}
}

func IsSupportedType(notificationType string) bool {
	for _, supported := range SupportedTypes() {
		if notificationType == supported {
			return true
		}
	}
	return false
}

// IsSettled reports whether a status is final: the notification was sent or failed.
func IsSettled(status string) bool {
	return status == StatusSent || status == StatusFailed
}
