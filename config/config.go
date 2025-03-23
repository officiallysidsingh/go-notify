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

// Global config instance
var AppConfig *Config

// Load config
func LoadConfig(path string) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(path)

	// Allow overriding with env variables
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("No config file found: %v", err)
	}

	AppConfig = &Config{
		GRPC: GRPCConfig{
			Port: viper.GetString("grpc.port"),
		},
		RabbitMQ: RabbitMQConfig{
			URL: viper.GetString("rabbitmq.url"),
		},
		Postgres: PostgresConfig{
			DataSourceName:  viper.GetString("postgres.DataSourceName"),
			MaxOpenConns:    viper.GetInt("postgres.MaxOpenConns"),
			MaxIdleConns:    viper.GetInt("postgres.MaxIdleConns"),
			ConnMaxLifetime: viper.GetDuration("postgres.ConnMaxLifetime"),
			ConnMaxIdleTime: viper.GetDuration("postgres.ConnMaxIdleTime"),
			ConnTimeout:     viper.GetDuration("postgres.ConnTimeout"),
		},
		Redis: RedisConfig{
			Addr:   viper.GetString("redis.addr"),
			Limit:  viper.GetInt("redis.limit"),
			Window: viper.GetString("redis.window"),
		},
		Metrics: MetricsConfig{
			Port: viper.GetString("metrics.port"),
		},
		Logging: LoggingConfig{
			Level: viper.GetString("logging.level"),
		},
		Ntfy: NtfyConfig{
			Topic: viper.GetString("ntfy.topic"),
		},
	}
}
