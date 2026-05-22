package app

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port              string
	Kubeconfig        string
	KBDir             string
	KBRefreshInterval time.Duration
	PodLogTailLines   int64
	ReadHeaderTimeout time.Duration
}

func LoadConfig() Config {
	return Config{
		Port:              envOrDefault("PORT", "8080"),
		Kubeconfig:        os.Getenv("KUBECONFIG"),
		KBDir:             envOrDefault("KB_DIR", "kb"),
		KBRefreshInterval: durationEnvOrDefault("KB_REFRESH_INTERVAL", 30*time.Second),
		PodLogTailLines:   int64EnvOrDefault("POD_LOG_TAIL_LINES", 200),
		ReadHeaderTimeout: durationEnvOrDefault("READ_HEADER_TIMEOUT", 5*time.Second),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnvOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func int64EnvOrDefault(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
