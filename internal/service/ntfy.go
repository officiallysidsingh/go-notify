// Package service holds the senders that deliver notifications to
// upstream providers. Currently only ntfy.
package service

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/notification"
)

const maxRetryAfter = time.Minute

// RetryAfterError says ntfy asked us to wait before trying again.
// Delay is zero when the server gave no usable hint.
type RetryAfterError struct {
	Delay time.Duration
}

func (e *RetryAfterError) Error() string { return "ntfy rate-limited the request" }

// NtfySender delivers push notifications through an ntfy server.
type NtfySender struct {
	client  *http.Client
	baseURL string
	topic   string
	token   string
}

// NewNtfySender builds a sender from the ntfy section of the configuration.
func NewNtfySender(cfg config.NtfyConfig) *NtfySender {
	return &NtfySender{
		client:  &http.Client{Timeout: cfg.Timeout},
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		topic:   cfg.Topic,
		token:   cfg.Token,
	}
}

// Send publishes the message to the configured topic. A rate-limited
// response yields a *RetryAfterError carrying the server's suggested delay.
func (s *NtfySender) Send(ctx context.Context, message notification.Message) error {
	req, err := s.newRequest(ctx, message)
	if err != nil {
		return err
	}

	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach ntfy: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return &RetryAfterError{Delay: parseRetryAfter(res.Header.Get("Retry-After"), time.Now())}
	case res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices:
		return nil
	default:
		return fmt.Errorf("failed to send push notification, status code: %d", res.StatusCode)
	}
}

func (s *NtfySender) newRequest(
	ctx context.Context,
	message notification.Message,
) (*http.Request, error) {
	topicURL, err := url.Parse(s.baseURL + "/" + url.PathEscape(s.topic))
	if err != nil {
		return nil, fmt.Errorf("invalid ntfy URL: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, topicURL.String(), strings.NewReader(message.Message),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build ntfy request: %w", err)
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Title", mime.QEncoding.Encode("utf-8", message.Title))
	if message.Priority != "" {
		req.Header.Set("X-Priority", message.Priority)
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	return req, nil
}

// parseRetryAfter reads a Retry-After value (integer seconds or HTTP date).
// It returns zero for missing, malformed or already-elapsed values and caps
// the result at maxRetryAfter.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return capRetryAfter(time.Duration(seconds) * time.Second)
	}
	if when, err := http.ParseTime(value); err == nil {
		return capRetryAfter(when.Sub(now))
	}
	return 0
}

func capRetryAfter(delay time.Duration) time.Duration {
	switch {
	case delay <= 0:
		return 0
	case delay > maxRetryAfter:
		return maxRetryAfter
	default:
		return delay
	}
}
