package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             string
	AllowedOrigins   []string
	RedisAddr        string
	RedisPassword    string
	RedisDB          int
	PostgresURL      string
	OpenAIAPIKey     string
	OpenAIModel      string
	OpenAIBaseURL    string
	SlackWebhookURL  string
	DiscordWebhookURL string
}

func Load() Config {
	return Config{
		Port:              getEnv("PORT", "8080"),
		AllowedOrigins:    splitCSV(getEnv("ALLOWED_ORIGINS", "http://localhost:5173")),
		RedisAddr:         getEnv("REDIS_ADDR", "redis:6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisDB:           getEnvInt("REDIS_DB", 0),
		PostgresURL:       getEnv("POSTGRES_URL", ""),
		OpenAIAPIKey:      getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:       getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBaseURL:     strings.TrimSuffix(getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
		SlackWebhookURL:   getEnv("SLACK_WEBHOOK_URL", ""),
		DiscordWebhookURL: getEnv("DISCORD_WEBHOOK_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		fmt.Printf("invalid %s value %q, using fallback %d\n", key, raw, fallback)
		return fallback
	}
	return v
}

func splitCSV(raw string) []string {
	items := strings.Split(raw, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}
