package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

const storedNotificationID = int64(7)

type fakeStore struct {
	insertErr error
	inserted  int
	statuses  map[int64]string
}

func (s *fakeStore) InsertNotification(_ context.Context, _, _, status string) (int64, error) {
	if s.insertErr != nil {
		return 0, s.insertErr
	}
	s.inserted++
	s.statuses[storedNotificationID] = status
	return storedNotificationID, nil
}

func (s *fakeStore) UpdateNotificationStatus(_ context.Context, id int64, status string) error {
	s.statuses[id] = status
	return nil
}

func (s *fakeStore) GetNotificationStatus(_ context.Context, id int64) (string, error) {
	storedStatus, found := s.statuses[id]
	if !found {
		return "", repository.ErrNotificationNotFound
	}
	return storedStatus, nil
}

type publishedMessage struct {
	routingKey string
	body       []byte
}

type fakePublisher struct {
	err       error
	published []publishedMessage
}

func (p *fakePublisher) Publish(_ context.Context, _, routingKey string, body []byte) error {
	if p.err != nil {
		return p.err
	}
	p.published = append(p.published, publishedMessage{routingKey, body})
	return nil
}

type fakeLimiter struct {
	allowed bool
	err     error
}

func (l *fakeLimiter) Allow(context.Context, string) (bool, error) {
	return l.allowed, l.err
}

type serverFixture struct {
	server    *NotificationServer
	store     *fakeStore
	publisher *fakePublisher
	limiter   *fakeLimiter
}

func newServerFixture() *serverFixture {
	fixture := &serverFixture{
		store:     &fakeStore{statuses: map[int64]string{}},
		publisher: &fakePublisher{},
		limiter:   &fakeLimiter{allowed: true},
	}
	fixture.server = NewNotificationServer(fixture.publisher, fixture.store, fixture.limiter)
	return fixture
}

func pushRequest() *pb.NotificationRequest {
	return &pb.NotificationRequest{
		UserId:  "user-1",
		Title:   "Hello",
		Message: "World",
		Type:    notification.TypePush,
	}
}

func TestSendNotificationReturnsNotificationID(t *testing.T) {
	fixture := newServerFixture()

	response, err := fixture.server.SendNotification(context.Background(), pushRequest())

	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, storedNotificationID, response.NotificationId)
}

func TestSendNotificationPublishesMessageRoutedByType(t *testing.T) {
	fixture := newServerFixture()

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	require.NoError(t, err)
	require.Len(t, fixture.publisher.published, 1)
	published := fixture.publisher.published[0]
	assert.Equal(t, notification.TypePush, published.routingKey)
	var message notification.Message
	require.NoError(t, json.Unmarshal(published.body, &message))
	assert.Equal(t, storedNotificationID, message.NotificationID)
}

func TestSendNotificationRejectsUnsupportedType(t *testing.T) {
	fixture := newServerFixture()
	request := pushRequest()
	request.Type = "fax"

	_, err := fixture.server.SendNotification(context.Background(), request)

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Zero(t, fixture.store.inserted)
}

func TestSendNotificationRejectsMissingUserID(t *testing.T) {
	fixture := newServerFixture()
	request := pushRequest()
	request.UserId = ""

	_, err := fixture.server.SendNotification(context.Background(), request)

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSendNotificationReportsRateLimitAsResourceExhausted(t *testing.T) {
	fixture := newServerFixture()
	fixture.limiter.allowed = false

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	assert.Zero(t, fixture.store.inserted)
}

func TestSendNotificationHidesStorageErrorDetails(t *testing.T) {
	fixture := newServerFixture()
	fixture.store.insertErr = errors.New("pq: password authentication failed")

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, err.Error(), "password")
}

func TestSendNotificationMarksNotificationFailedWhenPublishFails(t *testing.T) {
	fixture := newServerFixture()
	fixture.publisher.err = errors.New("broker down")

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Equal(t, notification.StatusFailed, fixture.store.statuses[storedNotificationID])
}

func TestGetNotificationStatusReturnsStoredStatus(t *testing.T) {
	fixture := newServerFixture()
	fixture.store.statuses[storedNotificationID] = notification.StatusSent

	response, err := fixture.server.GetNotificationStatus(
		context.Background(),
		&pb.StatusRequest{NotificationId: storedNotificationID},
	)

	require.NoError(t, err)
	assert.Equal(t, notification.StatusSent, response.Status)
}

func TestGetNotificationStatusReportsMissingNotificationAsNotFound(t *testing.T) {
	fixture := newServerFixture()

	_, err := fixture.server.GetNotificationStatus(
		context.Background(),
		&pb.StatusRequest{NotificationId: storedNotificationID},
	)

	assert.Equal(t, codes.NotFound, status.Code(err))
}
