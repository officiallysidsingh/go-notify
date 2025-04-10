package service

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
)

func SendPushNotification(topic, title, priority, message string) error {
	// - topic: ntfy topic
	// - title: title for the notification
	// - priority: priority of notification(1 - 5)
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

	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Printf("error closing response body: %v", err)
		}
	}()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("ailed to send push notification, status code: %d", res.StatusCode)
	}

	return nil
}
