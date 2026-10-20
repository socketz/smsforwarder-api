// Package tg is the embedded shared Telegram bot. It runs in the same process
// as the API (long polling) and handles the /bind flow: a user opens the bot via
// a deep link carrying a one-time code, and the bot ties that chat_id to the
// user's telegram channel by writing directly to the shared store — no internal
// network hop. It also exposes Send for the delivery engine.
package tg

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/socketz/smsforwarder-api/internal/store"
)

type Bot struct {
	b  *bot.Bot
	st *store.Store
}

// New builds the bot. The default handler processes /start and /bind.
func New(token string, st *store.Store) (*Bot, error) {
	t := &Bot{st: st}
	b, err := bot.New(token,
		bot.WithDefaultHandler(t.onUpdate),
		bot.WithErrorsHandler(transientAwareErrors),
	)
	if err != nil {
		return nil, fmt.Errorf("telegram new: %w", err)
	}
	t.b = b
	return t, nil
}

// Start begins long polling; blocks until ctx is cancelled.
func (t *Bot) Start(ctx context.Context) { t.b.Start(ctx) }

// telegramMaxChars is Telegram's per-message limit (4096); we split below it.
const telegramMaxChars = 4000

// Send delivers text to a chat id (used by the delivery engine's sender).
// Messages longer than Telegram's limit are split into multiple messages so
// long SMS are delivered in full instead of failing.
func (t *Bot) Send(ctx context.Context, chatID int64, text string) error {
	for _, chunk := range splitMessage(text, telegramMaxChars) {
		if _, err := t.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: chunk}); err != nil {
			return err
		}
	}
	return nil
}

// splitMessage breaks s into <=max-rune chunks, preferring to cut at a newline
// near the limit. Rune-safe (never splits a multi-byte character).
func splitMessage(s string, max int) []string {
	runes := []rune(s)
	if len(runes) <= max {
		return []string{s}
	}
	var out []string
	for len(runes) > max {
		cut := max
		for i := max; i > max-200 && i > 0; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

func (t *Bot) onUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}
	chatID := update.Message.Chat.ID
	code := parseBindCode(update.Message.Text)
	if code == "" {
		t.reply(ctx, chatID, "Hi! Tap \"Bind Telegram\" in the SMS Forwarder app and I'll link your account automatically.")
		return
	}

	userID, channelID, err := t.st.ClaimBindCode(ctx, code, store.Now())
	if err != nil {
		t.reply(ctx, chatID, "That bind code is invalid or has expired. Please get a new one in the app.")
		return
	}
	// Learn the chat_id from Telegram (authoritative) and store it on the channel.
	cfg := fmt.Sprintf(`{"chat_id":%d}`, chatID)
	if err := t.st.SetChannelConfig(ctx, channelID, cfg); err != nil {
		log.Printf("tg: SetChannelConfig: %v", err)
		t.reply(ctx, chatID, "Something went wrong while binding. Please try again later.")
		return
	}
	_ = userID
	t.reply(ctx, chatID, "✅ Bound successfully! Your SMS will be forwarded here from now on.")
}

func (t *Bot) reply(ctx context.Context, chatID int64, text string) {
	if err := t.Send(ctx, chatID, text); err != nil {
		log.Printf("tg: reply: %v", err)
	}
}

// parseBindCode extracts the code from "/start <code>" or "/bind <code>".
// Telegram deep links (t.me/<bot>?start=<code>) arrive as "/start <code>".
func parseBindCode(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) >= 2 && (fields[0] == "/start" || fields[0] == "/bind") {
		return fields[1]
	}
	return ""
}

// transientAwareErrors drops the routine long-poll network hiccups (Telegram
// closes idle connections; flaky routes blip); the library auto-reconnects.
func transientAwareErrors(err error) {
	if err == nil {
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return
	}
	s := err.Error()
	for _, sub := range []string{
		"connection reset by peer", "i/o timeout", "EOF",
		"broken pipe", "connection refused", "TLS handshake timeout",
	} {
		if strings.Contains(s, sub) {
			return
		}
	}
	log.Printf("telegram: %v", err)
}
