package config

import (
	"log"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type GRPCConfig struct {
	Port string
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
	Window string
}

type MetricsConfig struct {
	Port string
}

type LoggingConfig struct {
	Level string
}

type NtfyConfig struct {
	Topic string
}

// Holds all configuration values.
type Config struct {
	GRPC     GRPCConfig
	RabbitMQ RabbitMQConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Metrics  MetricsConfig
	Logging  LoggingConfig
	Ntfy     NtfyConfig
}

const defaultPostgresConnTimeout = 5 * time.Second

// Global config instance
var AppConfig *Config

// Load config
func LoadConfig(path string) {
	// A fresh instance keeps repeated loads from seeing each other's search paths.
	settings := viper.New()
	settings.SetDefault("postgres.ConnTimeout", defaultPostgresConnTimeout)
	settings.SetConfigName("config")
	settings.SetConfigType("yaml")
	settings.AddConfigPath(path)

	// Allow overriding with env variables
	settings.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	settings.AutomaticEnv()

	if err := settings.ReadInConfig(); err != nil {
		log.Printf("No config file found: %v", err)
	}

	AppConfig = &Config{
		GRPC: GRPCConfig{
			Port: settings.GetString("grpc.port"),
		},
		RabbitMQ: RabbitMQConfig{
			URL: settings.GetString("rabbitmq.url"),
		},
		Postgres: PostgresConfig{
			DataSourceName:  settings.GetString("postgres.DataSourceName"),
			MaxOpenConns:    settings.GetInt("postgres.MaxOpenConns"),
			MaxIdleConns:    settings.GetInt("postgres.MaxIdleConns"),
			ConnMaxLifetime: settings.GetDuration("postgres.ConnMaxLifetime"),
			ConnMaxIdleTime: settings.GetDuration("postgres.ConnMaxIdleTime"),
			ConnTimeout:     settings.GetDuration("postgres.ConnTimeout"),
		},
		Redis: RedisConfig{
			Addr:   settings.GetString("redis.addr"),
			Limit:  settings.GetInt("redis.limit"),
			Window: settings.GetString("redis.window"),
		},
		Metrics: MetricsConfig{
			Port: settings.GetString("metrics.port"),
		},
		Logging: LoggingConfig{
			Level: settings.GetString("logging.level"),
		},
		Ntfy: NtfyConfig{
			Topic: settings.GetString("ntfy.topic"),
		},
	}
}
