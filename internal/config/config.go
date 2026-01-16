package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port          string
	Backend       string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

func Load() Config {
	return Config{
		Port:          getEnv("PORT", "8080"),
		Backend:       getEnv("BACKEND", "memory"),
		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
