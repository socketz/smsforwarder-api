package store

import "context"

// ---- outbox ----

// EnqueueOutbox inserts a delivery task. When clientMsgID is non-empty the
// (channel_id, client_msg_id) unique index makes re-inserts a no-op (idempotent).
// Returns true if a new row was actually inserted.
func (s *Store) EnqueueOutbox(ctx context.Context, userID, channelID int64, sender, content string, smsTs int64, clientMsgID string) (bool, error) {
	now := Now()
	res, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO outbox(user_id, channel_id, sender, content, sms_ts, client_msg_id, attempts, next_at, created_at)
VALUES(?,?,?,?,?,?,0,?,?)`, userID, channelID, sender, content, smsTs, clientMsgID, now, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// EnqueueBroadcast inserts a delivery task for the global broadcast target (a
// configured Telegram channel/group). It uses the sentinel (user_id=0,
// channel_id=0) and the same (channel_id, client_msg_id) dedup as per-user
// rows, so retried SMS are not broadcast twice.
func (s *Store) EnqueueBroadcast(ctx context.Context, sender, content string, smsTs int64, clientMsgID string) (bool, error) {
	return s.EnqueueOutbox(ctx, 0, 0, sender, content, smsTs, clientMsgID)
}

// IsBroadcast reports whether an outbox row is the global broadcast item.
func IsBroadcast(it OutboxItem) bool {
	return it.UserID == 0 && it.ChannelID == 0
}

// BroadcastChannelType is the channel type recorded for broadcast deliveries.
const BroadcastChannelType = "broadcast"

func (s *Store) DueOutbox(ctx context.Context, now int64, limit int) ([]OutboxItem, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, channel_id, sender, content, sms_ts, client_msg_id, attempts, next_at, created_at
FROM outbox WHERE next_at<=? ORDER BY next_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		if err := rows.Scan(&it.ID, &it.UserID, &it.ChannelID, &it.Sender, &it.Content,
			&it.SmsTs, &it.ClientMsgID, &it.Attempts, &it.NextAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) DeleteOutbox(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM outbox WHERE id=?`, id)
	return err
}

// RescheduleOutbox bumps attempts and sets the next retry time.
func (s *Store) RescheduleOutbox(ctx context.Context, id int64, attempts int, nextAt int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE outbox SET attempts=?, next_at=? WHERE id=?`, attempts, nextAt, id)
	return err
}

// ---- delivery logs ----

func (s *Store) InsertDeliveryLog(ctx context.Context, l DeliveryLog) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO delivery_logs(user_id, channel_type, sender, status, detail, created_at)
VALUES(?,?,?,?,?,?)`, l.UserID, l.ChannelType, l.Sender, l.Status, l.Detail, Now())
	return err
}

// ---- telegram bind codes (used in P2) ----

func (s *Store) CreateBindCode(ctx context.Context, code string, userID, channelID, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tg_bind_codes(code, user_id, channel_id, expires_at, used) VALUES(?,?,?,?,0)`,
		code, userID, channelID, expiresAt)
	return err
}

// ClaimBindCode atomically marks a valid, unexpired, unused code as used and
// returns its (user_id, channel_id). Returns ErrNotFound otherwise.
func (s *Store) ClaimBindCode(ctx context.Context, code string, now int64) (userID, channelID int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id, channel_id FROM tg_bind_codes WHERE code=? AND used=0 AND expires_at>?`,
		code, now).Scan(&userID, &channelID)
	if err != nil {
		return 0, 0, ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, `UPDATE tg_bind_codes SET used=1 WHERE code=?`, code)
	return userID, channelID, err
}
