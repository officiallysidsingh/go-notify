package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/officiallysidsingh/go-notify/internal/notification"
)

const testTopic = "alerts"

func TestSendPostsMessageToTopic(t *testing.T) {
	var receivedPath, receivedTitle, receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedPath, receivedTitle, receivedBody = r.URL.Path, r.Header.Get("Title"), string(body)
	}))
	defer server.Close()
	sender := NewNtfySender(server.URL, testTopic, time.Second)

	err := sender.Send(context.Background(), notification.Message{Title: "Hello", Message: "World"})

	require.NoError(t, err)
	assert.Equal(t, "/"+testTopic, receivedPath)
	assert.Equal(t, "Hello", receivedTitle)
	assert.Equal(t, "World", receivedBody)
}

func TestSendReportsRejectedNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	sender := NewNtfySender(server.URL, testTopic, time.Second)

	err := sender.Send(context.Background(), notification.Message{})

	assert.ErrorContains(t, err, "429")
}

func TestSendGivesUpOnUnresponsiveServer(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	sender := NewNtfySender(server.URL, testTopic, 50*time.Millisecond)

	err := sender.Send(context.Background(), notification.Message{})

	assert.Error(t, err)
}
