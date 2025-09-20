package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Redis   RedisConfig
	Browser BrowserConfig
	Parser  ParserConfig
}

type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

type BrowserConfig struct {
	Headless bool
	Timeout  time.Duration
}

type ParserConfig struct {
	DelayBetweenRequests time.Duration
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	// Parse Redis DB
	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		redisDB = 0
	}

	// Parse headless mode
	headless, err := strconv.ParseBool(getEnv("HEADLESS", "true"))
	if err != nil {
		headless = true
	}

	// Parse timeout
	timeoutSeconds, err := strconv.Atoi(getEnv("TIMEOUT", "30"))
	if err != nil {
		timeoutSeconds = 30
	}

	// Parse delay between requests
	delaySeconds, err := strconv.Atoi(getEnv("DELAY_BETWEEN_REQUESTS", "2"))
	if err != nil {
		delaySeconds = 2
	}

	config := &Config{
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       redisDB,
		},
		Browser: BrowserConfig{
			Headless: headless,
			Timeout:  time.Duration(timeoutSeconds) * time.Second,
		},
		Parser: ParserConfig{
			DelayBetweenRequests: time.Duration(delaySeconds) * time.Second,
		},
	}

	return config, nil
}

// getEnv gets environment variable with default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}