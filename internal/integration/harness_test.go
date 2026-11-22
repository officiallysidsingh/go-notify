//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/app"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var (
	testRabbitmqURL = envOr("TEST_RABBITMQ_URL", "amqp://guest:guest@localhost:55672/")
	testRabbitmqAPI = envOr("TEST_RABBITMQ_API", "http://guest:guest@localhost:55673/api")
	testPostgresDSN = envOr("TEST_POSTGRES_DSN", "postgres://go_notify_test:go_notify_test@localhost:55432/go_notify_test?sslmode=disable")
	testRedisAddr   = envOr("TEST_REDIS_ADDR", "localhost:56379")
	migrationsDir   = "../../db/migrations"
)

const (
	testAPIKey      = "test-key"
	startupTimeout  = 10 * time.Second
	shutdownTimeout = 15 * time.Second
)

// stack is one test's isolated copy of the system: fresh schema, purged
// queues, flushed Redis and a stubbed ntfy.
type stack struct {
	t            *testing.T
	serverClient pb.NotificationServiceClient
	grpcConn     *grpc.ClientConn
	cfg          *config.Config
	ntfy         *ntfyStub
	redisProxy   *tcpProxy
	db           *sql.DB

	cancelServer context.CancelFunc
	cancelWorker context.CancelFunc
	serverDone   chan struct{}
	workerDone   chan struct{}
	serverErr    error // valid once serverDone is closed
	workerErr    error // valid once workerDone is closed

	brokerClients int // running processes holding a broker connection
}

func newStack(t *testing.T) *stack {
	t.Helper()
	s := &stack{t: t}

	s.db = openPostgres(t)
	resetSchema(t, s.db)
	purgeQueues(t)
	flushRedis(t)

	s.ntfy = newNtfyStub(t)
	s.redisProxy = newTCPProxy(t, testRedisAddr)
	s.cfg = &config.Config{
		GRPC:     config.GRPCConfig{Port: ":" + freePort(t), Reflection: false},
		Auth:     config.AuthConfig{APIKeys: "test-client:" + testAPIKey, Disabled: false},
		RabbitMQ: config.RabbitMQConfig{URL: testRabbitmqURL},
		Postgres: config.PostgresConfig{
			DataSourceName:  testPostgresDSN,
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: time.Minute,
			ConnMaxIdleTime: time.Minute,
			ConnTimeout:     3 * time.Second,
		},
		Redis:   config.RedisConfig{Addr: s.redisProxy.Addr(), Limit: 1000, Window: time.Minute},
		Metrics: config.MetricsConfig{Port: ":" + freePort(t)},
		Ntfy:    config.NtfyConfig{BaseURL: s.ntfy.URL, Topic: "test", Timeout: 2 * time.Second},
		Worker: config.WorkerConfig{
			Concurrency:  2,
			SendAttempts: 2,
			RetryDelay:   10 * time.Millisecond,
			MetricsPort:  ":" + freePort(t),
		},
		Sweep: config.SweepConfig{
			Interval:    time.Hour,
			StaleAfter:  5 * time.Minute,
			BatchSize:   100,
			MaxAttempts: 3,
		},
		Health: config.HealthConfig{Interval: 100 * time.Millisecond},
	}

	t.Cleanup(s.shutdown)
	return s
}

// shutdown stops whatever the test started and waits for it, so no goroutine
// logs after the test has finished.
func (s *stack) shutdown() {
	if s.cancelWorker != nil {
		s.cancelWorker()
	}
	if s.cancelServer != nil {
		s.cancelServer()
	}
	if s.grpcConn != nil {
		_ = s.grpcConn.Close()
	}
	s.waitDone(s.workerDone, "worker")
	s.waitDone(s.serverDone, "server")
	s.redisProxy.Stop(s.t)
	flushRedis(s.t)
	_ = s.db.Close()
}

func (s *stack) waitDone(done chan struct{}, name string) {
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(shutdownTimeout):
		s.t.Errorf("%s did not stop within %s", name, shutdownTimeout)
	}
}

func (s *stack) startServer(t *testing.T) pb.NotificationServiceClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelServer = cancel
	s.serverDone = make(chan struct{})
	s.brokerClients++
	logger := zaptest.NewLogger(t)
	go func() {
		defer close(s.serverDone)
		s.serverErr = app.RunServer(ctx, s.cfg, logger)
	}()

	s.grpcConn = s.dial(t, true)
	s.serverClient = pb.NewNotificationServiceClient(s.grpcConn)
	s.waitServing(t, s.grpcConn, 10*time.Second)
	return s.serverClient
}

// dial connects to the server's gRPC port, optionally sending the API key.
func (s *stack) dial(t *testing.T, withKey bool) *grpc.ClientConn {
	t.Helper()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if withKey {
		opts = append(opts, grpc.WithUnaryInterceptor(apiKeyInterceptor))
	}
	conn, err := grpc.NewClient("127.0.0.1"+s.cfg.GRPC.Port, opts...)
	require.NoError(t, err)
	if !withKey {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn
}

func apiKeyInterceptor(
	ctx context.Context, method string, req, reply any,
	cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
) error {
	ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", testAPIKey)
	return invoker(ctx, method, req, reply, cc, opts...)
}

func (s *stack) healthStatus(conn *grpc.ClientConn) (grpc_health_v1.HealthCheckResponse_ServingStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return grpc_health_v1.HealthCheckResponse_UNKNOWN, err
	}
	return res.Status, nil
}

func (s *stack) waitServing(t *testing.T, conn *grpc.ClientConn, timeout time.Duration) {
	t.Helper()
	var lastStatus grpc_health_v1.HealthCheckResponse_ServingStatus
	var lastErr error
	ok := assertEventually(timeout, 50*time.Millisecond, func() bool {
		lastStatus, lastErr = s.healthStatus(conn)
		return lastErr == nil && lastStatus == grpc_health_v1.HealthCheckResponse_SERVING
	})
	require.True(t, ok, "server never reported SERVING (last status %v, last error %v)", lastStatus, lastErr)
}

func (s *stack) startWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelWorker = cancel
	s.workerDone = make(chan struct{})
	s.brokerClients++
	logger := zaptest.NewLogger(t)
	go func() {
		defer close(s.workerDone)
		s.workerErr = app.RunWorker(ctx, s.cfg, logger)
	}()

	healthz := "http://127.0.0.1" + s.cfg.Worker.MetricsPort + "/healthz"
	require.Eventually(t, func() bool {
		res, err := http.Get(healthz)
		if err != nil {
			return false
		}
		defer func() { _ = res.Body.Close() }()
		return res.StatusCode == http.StatusOK
	}, startupTimeout, 50*time.Millisecond, "worker never became healthy")
}

// dropBrokerConnections force-closes every AMQP connection through the
// management API. The API lists connections only after its stats interval, so
// it first waits until every running process shows up.
func (s *stack) dropBrokerConnections(t *testing.T) {
	t.Helper()
	var names []string
	require.Eventually(t, func() bool {
		var conns []struct {
			Name string `json:"name"`
		}
		if err := mgmtJSON("/connections", &conns); err != nil {
			return false
		}
		names = names[:0]
		for _, c := range conns {
			names = append(names, c.Name)
		}
		return len(names) >= s.brokerClients
	}, 20*time.Second, 200*time.Millisecond, "broker connections never appeared in the management API")

	for _, name := range names {
		req, err := http.NewRequest(http.MethodDelete, testRabbitmqAPI+"/connections/"+url.PathEscape(name), nil)
		require.NoError(t, err)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Less(t, res.StatusCode, 300, "deleting connection %q", name)
	}
}

func mgmtJSON(path string, into any) error {
	res, err := http.Get(testRabbitmqAPI + path)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return io.ErrUnexpectedEOF
	}
	return json.NewDecoder(res.Body).Decode(into)
}

func (s *stack) send(t *testing.T) int64 {
	t.Helper()
	res, err := s.serverClient.SendNotification(context.Background(), &pb.NotificationRequest{
		UserId: "user-1", Title: "hello", Message: "world", Type: notification.TypePush,
	})
	require.NoError(t, err)
	require.True(t, res.Success)
	return res.NotificationId
}

func (s *stack) status(t *testing.T, id int64) string {
	t.Helper()
	res, err := s.serverClient.GetNotificationStatus(context.Background(), &pb.StatusRequest{NotificationId: id})
	require.NoError(t, err)
	return res.Status
}

func (s *stack) eventuallyStatus(t *testing.T, id int64, want string) {
	t.Helper()
	var got string
	ok := assertEventually(15*time.Second, 100*time.Millisecond, func() bool {
		res, err := s.serverClient.GetNotificationStatus(context.Background(), &pb.StatusRequest{NotificationId: id})
		if err != nil {
			return false
		}
		got = res.Status
		return got == want
	})
	if !ok {
		t.Fatalf("notification %d: status %q, want %q after 15s", id, got, want)
	}
}

func assertEventually(timeout, tick time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(tick)
	}
}

// queueDepth is the number of ready (not delivered or unacked) messages.
func queueDepth(t *testing.T, queue string) int {
	t.Helper()
	conn, err := amqp.Dial(testRabbitmqURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	require.NoError(t, err)
	q, err := ch.QueueDeclarePassive(queue, true, false, false, false, nil)
	require.NoError(t, err)
	return q.Messages
}

func workerMetrics(t *testing.T, s *stack) string {
	t.Helper()
	return httpGetBody(t, "http://127.0.0.1"+s.cfg.Worker.MetricsPort+"/metrics")
}

func httpGetBody(t *testing.T, target string) string {
	t.Helper()
	res, err := http.Get(target)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(body)
}

func openPostgres(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", testPostgresDSN)
	require.NoError(t, err)
	require.NoError(t, db.Ping(), "is Postgres up? run `make test-services-up`")
	return db
}

// dropSchema rolls every migration back. goose.Reset needs the version table,
// which a brand-new database does not have yet.
func dropSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	require.NoError(t, goose.SetDialect("postgres"))
	goose.SetLogger(goose.NopLogger())
	_, err := goose.EnsureDBVersion(db)
	require.NoError(t, err)
	require.NoError(t, goose.Reset(db, migrationsDir))
}

func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	dropSchema(t, db)
	require.NoError(t, goose.Up(db, migrationsDir))
}

func purgeQueues(t *testing.T) {
	t.Helper()
	conn, err := amqp.Dial(testRabbitmqURL)
	require.NoError(t, err, "is RabbitMQ up? run `make test-services-up`")
	defer func() { _ = conn.Close() }()
	raw, err := conn.Channel()
	require.NoError(t, err)
	require.NoError(t, rabbitmq.DeclareTopology(raw))
	queues := []string{rabbitmq.DeadLetterQueue}
	for _, kind := range notification.Types() {
		queues = append(queues, rabbitmq.QueueForType(kind))
	}
	for _, queue := range queues {
		_, err := raw.QueuePurge(queue, false)
		require.NoError(t, err)
	}
}

func flushRedis(t *testing.T) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr})
	defer func() { _ = client.Close() }()
	require.NoError(t, client.FlushAll(context.Background()).Err(), "is Redis up? run `make test-services-up`")
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	return port
}

// ntfyStub stands in for the ntfy server and records what it receives.
type ntfyStub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []ntfyRequest
	handler  http.HandlerFunc
}

type ntfyRequest struct {
	Title   string
	Body    string
	Path    string
	Headers http.Header
	At      time.Time
}

func newNtfyStub(t *testing.T) *ntfyStub {
	t.Helper()
	s := &ntfyStub{handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, ntfyRequest{
			Title: r.Header.Get("Title"), Body: string(body), Path: r.URL.Path,
			Headers: r.Header.Clone(), At: time.Now(),
		})
		handler := s.handler
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.Server.Close)
	return s
}

func (s *ntfyStub) SetHandler(h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

func (s *ntfyStub) Requests() []ntfyRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ntfyRequest(nil), s.requests...)
}

// tcpProxy forwards to a real upstream so a test can cut the connection
// without touching Docker.
type tcpProxy struct {
	listenAddr string
	upstream   string

	mu      sync.Mutex
	ln      net.Listener
	conns   []net.Conn
	wg      sync.WaitGroup
	stopped atomic.Bool
}

func newTCPProxy(t *testing.T, upstream string) *tcpProxy {
	t.Helper()
	p := &tcpProxy{listenAddr: "127.0.0.1:" + freePort(t), upstream: upstream}
	p.Start(t)
	return p
}

func (p *tcpProxy) Addr() string { return p.listenAddr }

func (p *tcpProxy) Start(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		return
	}
	var ln net.Listener
	require.Eventually(t, func() bool {
		var err error
		ln, err = net.Listen("tcp", p.listenAddr)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "could not rebind proxy port")
	p.ln = ln
	p.stopped.Store(false)
	p.wg.Add(1)
	go p.accept(ln)
}

func (p *tcpProxy) Stop(t *testing.T) {
	t.Helper()
	p.stopped.Store(true)
	p.mu.Lock()
	ln, conns := p.ln, p.conns
	p.ln, p.conns = nil, nil
	p.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	p.wg.Wait()
}

func (p *tcpProxy) accept(ln net.Listener) {
	defer p.wg.Done()
	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		upstream, err := net.Dial("tcp", p.upstream)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		if p.stopped.Load() {
			p.mu.Unlock()
			_ = client.Close()
			_ = upstream.Close()
			return
		}
		p.conns = append(p.conns, client, upstream)
		p.mu.Unlock()
		p.wg.Add(2)
		go p.pipe(client, upstream)
		go p.pipe(upstream, client)
	}
}

func (p *tcpProxy) pipe(dst, src net.Conn) {
	defer p.wg.Done()
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}
