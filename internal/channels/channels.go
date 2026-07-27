// Package channels defines the channel Sender abstraction and per-type config
// handling. A Sender takes a channel's config + an SMS and delivers it.
// v1 sender: telegram (shared bot). The registry starts empty; main registers
// the Telegram sender when a bot is configured.
package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrChannelUnbound signals the recipient is permanently unreachable for this
// channel's current config (e.g. Telegram 403: bot blocked / can't initiate /
// chat not found) — retrying won't help; the forward engine clears the binding
// so the user is prompted to re-bind.
var ErrChannelUnbound = errors.New("channel binding invalid")

const (
	TypeTelegram = "telegram"
)

// Message is one SMS to be delivered.
type Message struct {
	Sender    string
	Content   string
	Timestamp int64 // unix seconds
}

// Sender delivers a Message to one channel, given that channel's config JSON.
type Sender interface {
	Send(ctx context.Context, config string, msg Message) error
}

// Registry maps channel type -> Sender.
type Registry struct {
	senders map[string]Sender
}

func NewRegistry() *Registry {
	return &Registry{senders: map[string]Sender{}}
}

// Register adds/overrides a sender (main registers Telegram when configured).
func (r *Registry) Register(typ string, s Sender) { r.senders[typ] = s }

func (r *Registry) Sender(typ string) (Sender, bool) {
	s, ok := r.senders[typ]
	return s, ok
}

// KnownType reports whether typ is a valid channel type (even if its sender
// isn't wired yet).
func KnownType(typ string) bool {
	return typ == TypeTelegram
}

// telegramConfig mirrors the JSON stored in channels.config.
type telegramConfig struct {
	ChatID int64 `json:"chat_id"`
}

// ValidateConfig checks a user-supplied config for a channel type.
func ValidateConfig(typ, config string) error {
	switch typ {
	case TypeTelegram:
		// chat_id is filled by the /bind flow, so an empty config is allowed.
		if config != "" {
			var c telegramConfig
			if err := json.Unmarshal([]byte(config), &c); err != nil {
				return fmt.Errorf("invalid config json: %w", err)
			}
		}
	default:
		return fmt.Errorf("unknown channel type: %s", typ)
	}
	return nil
}

// Ready reports whether a channel has enough config to actually deliver.
func Ready(typ, config string) bool {
	switch typ {
	case TypeTelegram:
		var c telegramConfig
		_ = json.Unmarshal([]byte(config), &c)
		return c.ChatID != 0
	}
	return false
}

// formatText is the shared human-readable rendering of an SMS.
func formatText(msg Message) (title, body string) {
	t := time.Unix(msg.Timestamp, 0).Format("2006-01-02 15:04:05")
	title = "新短信"
	if msg.Sender != "" {
		title = "新短信 - " + msg.Sender
	}
	body = fmt.Sprintf("发件人: %s\n时间: %s\n\n%s", msg.Sender, t, msg.Content)
	return title, body
}
