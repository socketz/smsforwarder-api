package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// telegramSender delivers via the shared Telegram bot. It holds a send function
// (provided by the tg package) rather than the bot itself, to avoid an import
// cycle. chat_id comes from the channel config, filled by the /bind flow.
type telegramSender struct {
	send func(ctx context.Context, chatID int64, text string) error
}

// NewTelegramSender wires a Telegram sender around the bot's Send function.
func NewTelegramSender(send func(ctx context.Context, chatID int64, text string) error) Sender {
	return &telegramSender{send: send}
}

func (s *telegramSender) Send(ctx context.Context, config string, msg Message) error {
	var c telegramConfig
	if err := json.Unmarshal([]byte(config), &c); err != nil {
		return fmt.Errorf("bad telegram config: %w", err)
	}
	if c.ChatID == 0 {
		return fmt.Errorf("telegram channel not bound")
	}
	_, body := formatText(msg)
	err := s.send(ctx, c.ChatID, "📩 "+body)
	if err != nil && bindingDead(err) {
		// 403 etc.: recipient unreachable via the current bot (blocked / bot replaced / no chat)
		return fmt.Errorf("%w: %v", ErrChannelUnbound, err)
	}
	return err
}

// bindingDead reports whether a Telegram send error means the binding is
// permanently unusable (so we should clear it rather than retry).
func bindingDead(err error) bool {
	s := strings.ToLower(err.Error())
	for _, m := range []string{
		"forbidden", "bot was blocked", "chat not found",
		"can't initiate", "user is deactivated", "peer_id_invalid",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
