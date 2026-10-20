// Package forward is the delivery engine. It polls the outbox and delivers each
// item via the channel's Sender, retrying with exponential backoff. On success
// or final failure the outbox row is deleted (relay-and-forget: the SMS body
// never lingers); a metadata-only delivery log is written either way.
package forward

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/socketz/smsforwarder-api/internal/channels"
	"github.com/socketz/smsforwarder-api/internal/store"
)

const (
	maxAttempts  = 8
	pollInterval = 5 * time.Second
	batchSize    = 50
	backoffBase  = 10 * time.Second
	backoffCap   = 10 * time.Minute
)

type Engine struct {
	st  *store.Store
	reg *channels.Registry
}

func New(st *store.Store, reg *channels.Registry) *Engine {
	return &Engine{st: st, reg: reg}
}

// Run blocks until ctx is cancelled, processing due outbox items on a ticker.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.tick(ctx)
		}
	}
}

func (e *Engine) tick(ctx context.Context) {
	items, err := e.st.DueOutbox(ctx, store.Now(), batchSize)
	if err != nil {
		log.Printf("forward: DueOutbox: %v", err)
		return
	}
	for _, it := range items {
		e.deliver(ctx, it)
	}
}

func (e *Engine) deliver(ctx context.Context, it store.OutboxItem) {
	// Load the owning channel (scoped by user). If it's gone or disabled, drop.
	ch, err := e.st.GetChannel(ctx, it.UserID, it.ChannelID)
	if errors.Is(err, store.ErrNotFound) {
		_ = e.st.DeleteOutbox(ctx, it.ID) // channel deleted since enqueue
		return
	}
	if err != nil {
		log.Printf("forward: GetChannel: %v", err)
		return
	}
	if !ch.Enabled {
		_ = e.st.DeleteOutbox(ctx, it.ID)
		return
	}

	sender, ok := e.reg.Sender(ch.Type)
	if !ok {
		// No sender wired for this type (shouldn't happen for ready channels).
		e.finalizeFail(ctx, it, ch.Type, "no sender for channel type "+ch.Type)
		return
	}

	sendErr := sender.Send(ctx, ch.Config, channels.Message{
		Sender:    it.Sender,
		Content:   it.Content,
		Timestamp: it.SmsTs,
	})
	if sendErr == nil {
		_ = e.st.DeleteOutbox(ctx, it.ID)
		e.log(ctx, it.UserID, ch.Type, it.Sender, "ok", "")
		return
	}

	// Recipient permanently unreachable for the current config (e.g. Telegram 403):
	// clear the channel binding → the app shows "unbound" and asks the user to
	// re-bind; give up on this item (retrying won't help).
	if errors.Is(sendErr, channels.ErrChannelUnbound) {
		_ = e.st.ClearChannelBinding(ctx, ch.ID)
		_ = e.st.DeleteOutbox(ctx, it.ID)
		e.log(ctx, it.UserID, ch.Type, it.Sender, "fail", "binding cleared (no longer valid), please re-bind: "+sendErr.Error())
		return
	}

	attempts := it.Attempts + 1
	if attempts >= maxAttempts {
		e.finalizeFail(ctx, it, ch.Type, sendErr.Error())
		return
	}
	next := store.Now() + int64(backoff(attempts).Seconds())
	if err := e.st.RescheduleOutbox(ctx, it.ID, attempts, next); err != nil {
		log.Printf("forward: RescheduleOutbox: %v", err)
	}
}

// finalizeFail gives up: delete the row (burn the body) and log the failure.
func (e *Engine) finalizeFail(ctx context.Context, it store.OutboxItem, chType, detail string) {
	_ = e.st.DeleteOutbox(ctx, it.ID)
	e.log(ctx, it.UserID, chType, it.Sender, "fail", detail)
}

func (e *Engine) log(ctx context.Context, userID int64, chType, sender, status, detail string) {
	if err := e.st.InsertDeliveryLog(ctx, store.DeliveryLog{
		UserID: userID, ChannelType: chType, Sender: sender, Status: status, Detail: detail,
	}); err != nil {
		log.Printf("forward: InsertDeliveryLog: %v", err)
	}
}

// backoff returns base*2^(attempts-1), capped.
func backoff(attempts int) time.Duration {
	d := backoffBase
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= backoffCap {
			return backoffCap
		}
	}
	return d
}
