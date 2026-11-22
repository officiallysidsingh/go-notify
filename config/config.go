package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type GRPCConfig struct {
	Port       string
	Reflection bool
}

type AuthConfig struct {
	APIKeys  string
	Disabled bool
}

type RabbitMQConfig struct {
	URL string
}

type PostgresConfig struct {
	DataSourceName  string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	ConnTimeout     time.Duration
}

type RedisConfig struct {
	Addr   string
	Limit  int
	Window time.Duration
}

type MetricsConfig struct {
	Port string
}

type LoggingConfig struct {
	Level string
}

type NtfyConfig struct {
	BaseURL string
	Topic   string
	Token   string
	Timeout time.Duration
}

type WorkerConfig struct {
	Concurrency  int
	SendAttempts int
	RetryDelay   time.Duration
	MetricsPort  string
}

type SweepConfig struct {
	Interval    time.Duration
	StaleAfter  time.Duration
	BatchSize   int
	MaxAttempts int
}

type HealthConfig struct {
	Interval time.Duration
}

// Config holds all configuration values.
type Config struct {
	GRPC     GRPCConfig
	Auth     AuthConfig
	RabbitMQ RabbitMQConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Metrics  MetricsConfig
	Logging  LoggingConfig
	Ntfy     NtfyConfig
	Worker   WorkerConfig
	Sweep    SweepConfig
	Health   HealthConfig
}

const defaultPostgresConnTimeout = 5 * time.Second

func setDefaults(v *viper.Viper) {
	v.SetDefault("grpc.reflection", false)
	v.SetDefault("auth.apiKeys", "")
	v.SetDefault("auth.disabled", false)
	v.SetDefault("logging.level", "info")
	v.SetDefault("ntfy.baseURL", "https://ntfy.sh")
	v.SetDefault("ntfy.token", "")
	v.SetDefault("ntfy.timeout", 10*time.Second)
	v.SetDefault("worker.concurrency", 10)
	v.SetDefault("worker.sendAttempts", 3)
	v.SetDefault("worker.retryDelay", 2*time.Second)
	v.SetDefault("worker.metricsPort", ":9092")
	v.SetDefault("sweep.interval", time.Minute)
	v.SetDefault("sweep.staleAfter", 5*time.Minute)
	v.SetDefault("sweep.batchSize", 100)
	v.SetDefault("sweep.maxAttempts", 3)
	v.SetDefault("health.interval", 10*time.Second)
	v.SetDefault("postgres.ConnTimeout", defaultPostgresConnTimeout)
}

// Load reads config.yaml from path, with environment variables overriding file
// values. A missing file is fine; an unreadable or invalid file is an error.
func Load(path string) (*Config, error) {
	// A fresh instance keeps repeated loads from seeing each other's state.
	v := viper.New()
	setDefaults(v)
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(path)

	// Allow overriding with env variables
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	return fromViper(v), nil
}

// fromViper assembles a Config from the resolved viper state.
func fromViper(v *viper.Viper) *Config {
	return &Config{
		GRPC:     GRPCConfig{Port: v.GetString("grpc.port"), Reflection: v.GetBool("grpc.reflection")},
		Auth:     AuthConfig{APIKeys: v.GetString("auth.apiKeys"), Disabled: v.GetBool("auth.disabled")},
		RabbitMQ: RabbitMQConfig{URL: v.GetString("rabbitmq.url")},
		Postgres: postgresFrom(v),
		Redis: RedisConfig{
			Addr:   v.GetString("redis.addr"),
			Limit:  v.GetInt("redis.limit"),
			Window: v.GetDuration("redis.window"),
		},
		Metrics: MetricsConfig{Port: v.GetString("metrics.port")},
		Logging: LoggingConfig{Level: v.GetString("logging.level")},
		Ntfy:    ntfyFrom(v),
		Worker:  workerFrom(v),
		Sweep:   sweepFrom(v),
		Health:  HealthConfig{Interval: v.GetDuration("health.interval")},
	}
}

func postgresFrom(v *viper.Viper) PostgresConfig {
	return PostgresConfig{
		DataSourceName:  v.GetString("postgres.DataSourceName"),
		MaxOpenConns:    v.GetInt("postgres.MaxOpenConns"),
		MaxIdleConns:    v.GetInt("postgres.MaxIdleConns"),
		ConnMaxLifetime: v.GetDuration("postgres.ConnMaxLifetime"),
		ConnMaxIdleTime: v.GetDuration("postgres.ConnMaxIdleTime"),
		ConnTimeout:     v.GetDuration("postgres.ConnTimeout"),
	}
}

func ntfyFrom(v *viper.Viper) NtfyConfig {
	return NtfyConfig{
		BaseURL: v.GetString("ntfy.baseURL"),
		Topic:   v.GetString("ntfy.topic"),
		Token:   v.GetString("ntfy.token"),
		Timeout: v.GetDuration("ntfy.timeout"),
	}
}

func workerFrom(v *viper.Viper) WorkerConfig {
	return WorkerConfig{
		Concurrency:  v.GetInt("worker.concurrency"),
		SendAttempts: v.GetInt("worker.sendAttempts"),
		RetryDelay:   v.GetDuration("worker.retryDelay"),
		MetricsPort:  v.GetString("worker.metricsPort"),
	}
}

func sweepFrom(v *viper.Viper) SweepConfig {
	return SweepConfig{
		Interval:    v.GetDuration("sweep.interval"),
		StaleAfter:  v.GetDuration("sweep.staleAfter"),
		BatchSize:   v.GetInt("sweep.batchSize"),
		MaxAttempts: v.GetInt("sweep.maxAttempts"),
	}
}
