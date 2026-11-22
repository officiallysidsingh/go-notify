package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

const storedNotificationID = int64(7)

type fakeStore struct {
	insertErr     error
	markQueuedErr error
	updateErr     error
	getStatusErr  error
	inserted      int
	captured      notification.Message
	statuses      map[int64]string
}

func (s *fakeStore) InsertNotification(_ context.Context, message notification.Message) (int64, error) {
	s.captured = message
	if s.insertErr != nil {
		return 0, s.insertErr
	}
	s.inserted++
	s.statuses[storedNotificationID] = notification.StatusPending
	return storedNotificationID, nil
}

func (s *fakeStore) MarkQueued(_ context.Context, id int64) error {
	if s.markQueuedErr != nil {
		return s.markQueuedErr
	}
	s.statuses[id] = notification.StatusQueued
	return nil
}

func (s *fakeStore) UpdateNotificationStatus(_ context.Context, id int64, status string) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.statuses[id] = status
	return nil
}

func (s *fakeStore) GetNotificationStatus(_ context.Context, id int64) (string, error) {
	if s.getStatusErr != nil {
		return "", s.getStatusErr
	}
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
	deps      Dependencies
}

func newServerFixture(t *testing.T) *serverFixture {
	return newServerFixtureWithLogger(t, zaptest.NewLogger(t))
}

func newServerFixtureWithLogger(t *testing.T, logger *zap.Logger) *serverFixture {
	t.Helper()
	fixture := &serverFixture{
		store:     &fakeStore{statuses: map[int64]string{}},
		publisher: &fakePublisher{},
		limiter:   &fakeLimiter{allowed: true},
	}
	fixture.deps = Dependencies{
		Publisher:   fixture.publisher,
		Store:       fixture.store,
		RateLimiter: fixture.limiter,
		Metrics:     metrics.NewServer(prometheus.NewRegistry()),
		Logger:      logger,
	}
	fixture.server = NewNotificationServer(fixture.deps)
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
	fixture := newServerFixture(t)

	response, err := fixture.server.SendNotification(context.Background(), pushRequest())

	require.NoError(t, err)
	assert.True(t, response.Success) //nolint:staticcheck // deprecated field kept populated
	assert.Equal(t, storedNotificationID, response.NotificationId)
}

func TestSendNotificationPublishesMessageRoutedByType(t *testing.T) {
	fixture := newServerFixture(t)

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
	fixture := newServerFixture(t)
	request := pushRequest()
	request.Type = "fax"

	_, err := fixture.server.SendNotification(context.Background(), request)

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Zero(t, fixture.store.inserted)
}

func TestSendNotificationRejectsMissingUserID(t *testing.T) {
	fixture := newServerFixture(t)
	request := pushRequest()
	request.UserId = ""

	_, err := fixture.server.SendNotification(context.Background(), request)

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSendNotificationReportsRateLimitAsResourceExhausted(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.limiter.allowed = false

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	assert.Zero(t, fixture.store.inserted)
}

func TestSendNotificationHidesStorageErrorDetails(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.store.insertErr = errors.New("pq: password authentication failed")

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, err.Error(), "password")
}

func TestSendNotificationMarksNotificationFailedWhenPublishFails(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.publisher.err = errors.New("broker down")

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Equal(t, notification.StatusFailed, fixture.store.statuses[storedNotificationID])
}

func TestGetNotificationStatusReturnsStoredStatus(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.store.statuses[storedNotificationID] = notification.StatusSent

	response, err := fixture.server.GetNotificationStatus(
		context.Background(),
		&pb.StatusRequest{NotificationId: storedNotificationID},
	)

	require.NoError(t, err)
	assert.Equal(t, notification.StatusSent, response.Status)
}

func TestGetNotificationStatusReportsMissingNotificationAsNotFound(t *testing.T) {
	fixture := newServerFixture(t)

	_, err := fixture.server.GetNotificationStatus(
		context.Background(),
		&pb.StatusRequest{NotificationId: storedNotificationID},
	)

	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestSendNotificationRejectsEmailBeforeStoring(t *testing.T) {
	fixture := newServerFixture(t)
	request := pushRequest()
	request.Type = notification.TypeEmail

	_, err := fixture.server.SendNotification(context.Background(), request)

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Zero(t, fixture.store.inserted)
}

func TestSendNotificationMarksNotificationQueuedAfterPublish(t *testing.T) {
	fixture := newServerFixture(t)

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	require.NoError(t, err)
	assert.Equal(t, notification.StatusQueued, fixture.store.statuses[storedNotificationID])
}

func TestSendNotificationSucceedsWhenMarkQueuedFails(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.store.markQueuedErr = errors.New("db down")

	response, err := fixture.server.SendNotification(context.Background(), pushRequest())

	require.NoError(t, err)
	assert.True(t, response.Success) //nolint:staticcheck // deprecated field kept populated
	assert.Equal(t, notification.StatusPending, fixture.store.statuses[storedNotificationID])
}

func TestSendNotificationStoresTitleTypeAndPriority(t *testing.T) {
	fixture := newServerFixture(t)
	request := pushRequest()
	request.Priority = "3"

	_, err := fixture.server.SendNotification(context.Background(), request)

	require.NoError(t, err)
	assert.Equal(t, "Hello", fixture.store.captured.Title)
	assert.Equal(t, notification.TypePush, fixture.store.captured.Type)
	assert.Equal(t, "3", fixture.store.captured.Priority)
}

func TestSendNotificationFailsClosedWhenRateLimiterIsDown(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.limiter.err = errors.New("redis down")

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.NotContains(t, err.Error(), "redis")
	assert.Zero(t, fixture.store.inserted)
}

func TestSendNotificationCountsRateLimitedRequests(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.limiter.allowed = false

	_, _ = fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, 1.0, testutil.ToFloat64(fixture.deps.Metrics.RateLimited))
	assert.Equal(t, 0.0, testutil.ToFloat64(fixture.deps.Metrics.Received))
}

func TestGetNotificationStatusRejectsNonPositiveID(t *testing.T) {
	fixture := newServerFixture(t)

	for _, id := range []int64{0, -1} {
		_, err := fixture.server.GetNotificationStatus(
			context.Background(), &pb.StatusRequest{NotificationId: id},
		)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "id %d", id)
	}
}

func TestGetNotificationStatusHidesStorageErrorDetails(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.store.getStatusErr = errors.New("pq: password authentication failed")

	_, err := fixture.server.GetNotificationStatus(
		context.Background(), &pb.StatusRequest{NotificationId: storedNotificationID},
	)

	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, err.Error(), "password")
}

func TestSendNotificationLogsCarryNotificationID(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	fixture := newServerFixtureWithLogger(t, zap.New(core))
	fixture.publisher.err = errors.New("broker down")

	_, _ = fixture.server.SendNotification(context.Background(), pushRequest())

	entries := logs.FilterField(zap.Int64("notification_id", storedNotificationID)).All()
	require.Len(t, entries, 1)
	assert.Equal(t, zap.ErrorLevel, entries[0].Level)
}

func TestSendNotificationLogsWhenMarkingFailedAlsoFails(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	fixture := newServerFixtureWithLogger(t, zap.New(core))
	fixture.publisher.err = errors.New("broker down")
	fixture.store.updateErr = errors.New("database down")

	_, err := fixture.server.SendNotification(context.Background(), pushRequest())

	assert.Equal(t, codes.Unavailable, status.Code(err))
	entries := logs.FilterMessage("failed to mark notification failed").All()
	require.Len(t, entries, 1)
	assert.Equal(t, int64(storedNotificationID), entries[0].ContextMap()["notification_id"])
}
