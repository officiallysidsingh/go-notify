package config

import (
	"log"
	"strings"

	"github.com/spf13/viper"
)

type GRPCConfig struct {
	Port string
}

type RabbitMQConfig struct {
	URL   string
	Queue string
}

type PostgresConfig struct {
	DSN string
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
			URL:   viper.GetString("rabbitmq.url"),
			Queue: viper.GetString("rabbitmq.queue"),
		},
		Postgres: PostgresConfig{
			DSN: viper.GetString("postgres.dsn"),
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
