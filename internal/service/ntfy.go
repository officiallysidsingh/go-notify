package service

import (
	"bytes"
	"fmt"
	"net/http"
)

func SendPushNotification(topic, title, priority, message string) error {
	// - topic: your ntfy topic
	// - title: title for the notification
	// - message: the notification body

	url := fmt.Sprintf("https://ntfy.sh/%s", topic)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(message)))
	if err != nil {
		return err
	}

	req.Header.Set("Title", title)
	req.Header.Set("X-Priority", priority)
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
