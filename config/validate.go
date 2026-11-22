package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

type validator struct {
	errs []error
}

func (v *validator) fail(key, reason string) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", key, reason))
}

func (v *validator) required(key, value string) {
	if value == "" {
		v.fail(key, "must not be empty")
	}
}

func (v *validator) positive(key string, value int) {
	if value <= 0 {
		v.fail(key, "must be greater than 0")
	}
}

func (v *validator) positiveDuration(key string, value time.Duration) {
	if value <= 0 {
		v.fail(key, "must be greater than 0")
	}
}

// scheme checks a non-empty value parses as a URL with one of the schemes.
func (v *validator) scheme(key, value string, schemes ...string) {
	if value == "" {
		return
	}
	u, err := url.Parse(value)
	if err != nil {
		v.fail(key, "is not a valid URL")
		return
	}
	for _, s := range schemes {
		if u.Scheme == s {
			return
		}
	}
	v.fail(key, fmt.Sprintf("scheme must be one of %v", schemes))
}

func (v *validator) err() error {
	return errors.Join(v.errs...)
}

// ValidateServer reports every problem with the settings the server needs.
func (c *Config) ValidateServer() error {
	var v validator
	v.required("grpc.port", c.GRPC.Port)
	v.required("metrics.port", c.Metrics.Port)
	v.required("rabbitmq.url", c.RabbitMQ.URL)
	v.scheme("rabbitmq.url", c.RabbitMQ.URL, "amqp", "amqps")
	v.required("postgres.DataSourceName", c.Postgres.DataSourceName)
	v.required("redis.addr", c.Redis.Addr)
	v.positive("redis.limit", c.Redis.Limit)
	v.positiveDuration("redis.window", c.Redis.Window)
	v.positiveDuration("sweep.interval", c.Sweep.Interval)
	v.positiveDuration("sweep.staleAfter", c.Sweep.StaleAfter)
	v.positive("sweep.batchSize", c.Sweep.BatchSize)
	v.positive("sweep.maxAttempts", c.Sweep.MaxAttempts)
	v.positiveDuration("health.interval", c.Health.Interval)
	if !c.Auth.Disabled {
		v.required("auth.apiKeys", c.Auth.APIKeys)
	}
	return v.err()
}

// ValidateWorker reports every problem with the settings the worker needs.
func (c *Config) ValidateWorker() error {
	var v validator
	v.required("rabbitmq.url", c.RabbitMQ.URL)
	v.scheme("rabbitmq.url", c.RabbitMQ.URL, "amqp", "amqps")
	v.required("postgres.DataSourceName", c.Postgres.DataSourceName)
	v.required("ntfy.topic", c.Ntfy.Topic)
	v.required("ntfy.baseURL", c.Ntfy.BaseURL)
	v.scheme("ntfy.baseURL", c.Ntfy.BaseURL, "http", "https")
	v.required("worker.metricsPort", c.Worker.MetricsPort)
	v.positive("worker.concurrency", c.Worker.Concurrency)
	v.positive("worker.sendAttempts", c.Worker.SendAttempts)
	v.positiveDuration("worker.retryDelay", c.Worker.RetryDelay)
	v.positiveDuration("ntfy.timeout", c.Ntfy.Timeout)
	return v.err()
}
