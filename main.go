// Command smsforwarder-api is the backend for the SMS Forwarder app: it receives
// SMS uploaded by the phone client, authenticates the user, and fans each message
// out to that user's configured channels (Telegram). An optional
// operator admin console runs on a separate (localhost-by-default) address.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/socketz/smsforwarder-api/internal/admin"
	"github.com/socketz/smsforwarder-api/internal/api"
	"github.com/socketz/smsforwarder-api/internal/channels"
	"github.com/socketz/smsforwarder-api/internal/config"
	"github.com/socketz/smsforwarder-api/internal/forward"
	"github.com/socketz/smsforwarder-api/internal/ratelimit"
	"github.com/socketz/smsforwarder-api/internal/store"
	"github.com/socketz/smsforwarder-api/internal/tg"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Bot config is DB-first (admin console), env fallback.
	botToken, botUsername := cfg.BotToken, cfg.BotUsername
	if v, _ := st.GetSetting(context.Background(), "bot_token"); v != "" {
		botToken = v
	}
	if v, _ := st.GetSetting(context.Background(), "bot_username"); v != "" {
		botUsername = v
	}

	reg := channels.NewRegistry()
	limiter := ratelimit.New(cfg.RateLimitPerMin)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Shared Telegram bot (optional): handles /bind via long polling + provides the sender.
	if botToken != "" {
		tgBot, err := tg.New(botToken, st)
		if err != nil {
			log.Fatalf("telegram bot: %v", err)
		}
		reg.Register(channels.TypeTelegram, channels.NewTelegramSender(tgBot.Send))
		go tgBot.Start(ctx)
		log.Printf("telegram bot enabled (@%s)", botUsername)
	} else {
		log.Printf("telegram bot disabled (no bot token)")
	}

	// Delivery engine (polls the outbox, retries with backoff).
	go forward.New(st, reg).Run(ctx)

	apiSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewServer(st, reg, limiter, botUsername).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("api listening on %s (db=%s)", cfg.HTTPAddr, cfg.DBPath)
		if err := apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("api server: %v", err)
		}
	}()

	// Admin console (optional): only when creds are configured.
	var adminSrv *http.Server
	if cfg.AdminUser != "" && cfg.AdminPassword != "" {
		adminSrv = &http.Server{
			Addr:              cfg.AdminAddr,
			Handler:           admin.New(st, cfg.AdminUser, cfg.AdminPassword).Routes(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			log.Printf("admin console on %s", cfg.AdminAddr)
			if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("admin server: %v", err)
			}
		}()
	} else {
		log.Printf("admin console disabled (ADMIN_USER/ADMIN_PASSWORD not set)")
	}

	<-ctx.Done()
	log.Println("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = apiSrv.Shutdown(shutdownCtx)
	if adminSrv != nil {
		_ = adminSrv.Shutdown(shutdownCtx)
	}
}
