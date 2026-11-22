package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/officiallysidsingh/go-notify/internal/notification"
)

const NtfyBaseURL = "https://ntfy.sh"

// NtfySender delivers push notifications through an ntfy server.
type NtfySender struct {
	client   *http.Client
	topicURL string
}

func NewNtfySender(baseURL, topic string, timeout time.Duration) *NtfySender {
	return &NtfySender{
		client:   &http.Client{Timeout: timeout},
		topicURL: baseURL + "/" + url.PathEscape(topic),
	}
}

func (s *NtfySender) Send(ctx context.Context, message notification.Message) error {
	req, err := s.newRequest(ctx, message)
	if err != nil {
		return err
	}

	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach ntfy: %w", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Printf("error closing response body: %v", err)
		}
	}()

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("failed to send push notification, status code: %d", res.StatusCode)
	}
	return nil
}

func (s *NtfySender) newRequest(
	ctx context.Context,
	message notification.Message,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, s.topicURL, strings.NewReader(message.Message),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build ntfy request: %w", err)
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Title", message.Title)
	if message.Priority != "" {
		req.Header.Set("X-Priority", message.Priority)
	}
	return req, nil
}
