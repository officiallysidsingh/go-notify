package service

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/notification"
)

const testTopic = "alerts"

func senderFor(serverURL string) *NtfySender {
	return NewNtfySender(config.NtfyConfig{BaseURL: serverURL, Topic: testTopic, Timeout: time.Second})
}

func rateLimitedServer(retryAfter string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
}

func retryDelay(t *testing.T, retryAfter string) time.Duration {
	t.Helper()
	server := rateLimitedServer(retryAfter)
	defer server.Close()

	err := senderFor(server.URL).Send(context.Background(), notification.Message{})

	var retryErr *RetryAfterError
	require.True(t, errors.As(err, &retryErr), "expected RetryAfterError, got %v", err)
	return retryErr.Delay
}

func captureHeaders(t *testing.T, cfg config.NtfyConfig, msg notification.Message) http.Header {
	t.Helper()
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer server.Close()
	cfg.BaseURL = server.URL
	cfg.Timeout = time.Second

	require.NoError(t, NewNtfySender(cfg).Send(context.Background(), msg))
	return got
}

func TestSendPostsMessageToTopic(t *testing.T) {
	var receivedPath, receivedTitle, receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedPath, receivedTitle, receivedBody = r.URL.Path, r.Header.Get("Title"), string(body)
	}))
	defer server.Close()

	err := senderFor(server.URL).Send(
		context.Background(), notification.Message{Title: "Hello", Message: "World"},
	)

	require.NoError(t, err)
	assert.Equal(t, "/"+testTopic, receivedPath)
	assert.Equal(t, "Hello", receivedTitle)
	assert.Equal(t, "World", receivedBody)
}

func TestSendReportsRejectedNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	err := senderFor(server.URL).Send(context.Background(), notification.Message{})

	assert.ErrorContains(t, err, "500")
	var retryErr *RetryAfterError
	assert.False(t, errors.As(err, &retryErr))
}

func TestSendGivesUpOnUnresponsiveServer(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	sender := NewNtfySender(config.NtfyConfig{
		BaseURL: server.URL, Topic: testTopic, Timeout: 50 * time.Millisecond,
	})

	err := sender.Send(context.Background(), notification.Message{})

	assert.Error(t, err)
}

func TestSendAuthenticatesWithToken(t *testing.T) {
	headers := captureHeaders(t, config.NtfyConfig{Topic: testTopic, Token: "tk_abc"}, notification.Message{})

	assert.Equal(t, "Bearer tk_abc", headers.Get("Authorization"))
}

func TestSendOmitsAuthorizationWithoutToken(t *testing.T) {
	headers := captureHeaders(t, config.NtfyConfig{Topic: testTopic}, notification.Message{})

	assert.Empty(t, headers.Get("Authorization"))
}

func TestSendEncodesNonASCIITitle(t *testing.T) {
	headers := captureHeaders(
		t, config.NtfyConfig{Topic: testTopic}, notification.Message{Title: "Größe"},
	)

	decoded, err := (&mime.WordDecoder{}).DecodeHeader(headers.Get("Title"))

	require.NoError(t, err)
	assert.Equal(t, "Größe", decoded)
	assert.NotEqual(t, "Größe", headers.Get("Title"))
}

func TestSendOmitsPriorityHeaderWhenUnset(t *testing.T) {
	headers := captureHeaders(t, config.NtfyConfig{Topic: testTopic}, notification.Message{})

	assert.Empty(t, headers.Values("X-Priority"))
}

func TestSendReportsRetryAfterSecondsOnRateLimit(t *testing.T) {
	assert.Equal(t, 7*time.Second, retryDelay(t, "7"))
}

func TestSendCapsRetryAfter(t *testing.T) {
	assert.Equal(t, time.Minute, retryDelay(t, "3600"))
}

func TestSendReportsNoHintForUnparseableRetryAfter(t *testing.T) {
	for name, value := range map[string]string{"absent": "", "text": "soon", "negative": "-5"} {
		t.Run(name, func(t *testing.T) {
			assert.Zero(t, retryDelay(t, value))
		})
	}
}

func TestSendEscapesTopicInURL(t *testing.T) {
	var escapedPath string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
	}))
	defer server.Close()
	sender := NewNtfySender(config.NtfyConfig{BaseURL: server.URL, Topic: "a b/c", Timeout: time.Second})

	require.NoError(t, sender.Send(context.Background(), notification.Message{}))

	assert.Equal(t, "/a%20b%2Fc", escapedPath)
}

func TestSendStopsWhenContextIsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := senderFor(server.URL).Send(ctx, notification.Message{})

	assert.ErrorIs(t, err, context.Canceled)
}

func TestSendReportsInvalidBaseURL(t *testing.T) {
	sender := NewNtfySender(config.NtfyConfig{BaseURL: "://bad", Topic: testTopic, Timeout: time.Second})

	err := sender.Send(context.Background(), notification.Message{})

	assert.ErrorContains(t, err, "invalid")
}

func TestParseRetryAfterParsesIntegerSeconds(t *testing.T) {
	assert.Equal(t, 30*time.Second, parseRetryAfter(" 30 ", time.Now()))
}

func TestParseRetryAfterParsesHTTPDate(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

	assert.Equal(t, 20*time.Second, parseRetryAfter(now.Add(20*time.Second).Format(http.TimeFormat), now))
	assert.Equal(t, time.Minute, parseRetryAfter(now.Add(time.Hour).Format(http.TimeFormat), now))
}

func TestParseRetryAfterReturnsZeroForPastDate(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

	assert.Zero(t, parseRetryAfter(now.Add(-time.Hour).Format(http.TimeFormat), now))
}

func TestParseRetryAfterReturnsZeroForMalformed(t *testing.T) {
	for _, value := range []string{"", "soon", "-5", "0", "1.5"} {
		assert.Zero(t, parseRetryAfter(value, time.Now()), value)
	}
}
