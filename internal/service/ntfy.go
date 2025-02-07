package service

import (
	"bytes"
	"fmt"
	"net/http"
)

// SendPushNotification sends a push notification using ntfy.
// - topic: your ntfy topic
// - title: title for the notification
// - message: the notification body
func SendPushNotification(topic, title, message string) error {
	url := fmt.Sprintf("https://ntfy.sh/%s", topic)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(message)))
	if err != nil {
		return err
	}

	req.Header.Set("Title", title)
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{}

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("ailed to send push notification, status code: %d", res.StatusCode)
	}

	return nil
}
