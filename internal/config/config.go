// Package config loads bootstrap configuration from environment variables.
// Everything else (per-user data, channels) lives in the database.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	DBPath   string // SQLite file path
	HTTPAddr string // public API bind address (put behind Cloudflare / reverse proxy + TLS)

	// Basic per-user rate limit for /api/sms (requests per minute).
	RateLimitPerMin int

	// Shared Telegram bot. When BotToken is empty, the Telegram channel is
	// disabled (bot doesn't start, bind-code endpoint returns 501).
	// These are env fallbacks; the admin console can override them in the DB.
	BotToken    string
	BotUsername string // without leading @, used to build t.me deep links

	// Admin console. Disabled when AdminUser/AdminPassword are empty.
	AdminAddr     string // bind address; keep on localhost unless fronted by TLS
	AdminUser     string
	AdminPassword string
}

func Load() Config {
	return Config{
		DBPath:          env("DB_PATH", "./smsforwarder.db"),
		HTTPAddr:        env("HTTP_ADDR", "0.0.0.0:8090"),
		RateLimitPerMin: envInt("RATE_LIMIT_PER_MIN", 60),
		BotToken:        env("BOT_TOKEN", ""),
		BotUsername:     env("BOT_USERNAME", ""),
		AdminAddr:       env("ADMIN_ADDR", "127.0.0.1:8091"),
		AdminUser:       env("ADMIN_USER", ""),
		AdminPassword:   env("ADMIN_PASSWORD", ""),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
