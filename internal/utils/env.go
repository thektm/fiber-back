package utils

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// LoadEnv loads environment variables from .env file
func LoadEnv() error {
	// Ignore error if .env file doesn't exist (e.g. in production)
	_ = godotenv.Load()
	return nil
}

// GetEnv returns the value of an environment variable or a default value
func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// GetEnvInt returns the value of an environment variable as an integer or a default value
func GetEnvInt(key string, defaultValue int) int {
	valueStr := GetEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}
