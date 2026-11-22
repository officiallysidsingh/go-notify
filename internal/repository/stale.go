package repository

import "github.com/officiallysidsingh/go-notify/internal/notification"

// StaleNotification is a pending notification claimed by the sweep for redelivery.
type StaleNotification struct {
	notification.Message
	SweepAttempts int
}
