package notification

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

func Types() []string {
	return []string{TypeEmail, TypeSMS, TypePush}
}

func IsSupportedType(notificationType string) bool {
	for _, supported := range Types() {
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
