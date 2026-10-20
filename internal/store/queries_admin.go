package store

import (
	"context"
	"database/sql"
	"strconv"
)

// ---- admin: users ----

type UserRow struct {
	User
	Channels int
	Devices  int
}

func (s *Store) ListUsersWithCounts(ctx context.Context) ([]UserRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT u.id, u.username, u.status, u.created_at,
  (SELECT COUNT(*) FROM channels c WHERE c.user_id=u.id) AS chans,
  (SELECT COUNT(*) FROM devices d WHERE d.user_id=u.id AND d.revoked=0) AS devs
FROM users u ORDER BY u.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var r UserRow
		if err := rows.Scan(&r.ID, &r.Username, &r.Status, &r.CreatedAt, &r.Channels, &r.Devices); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type DeviceRow struct {
	ID         int64
	Label      string
	CreatedAt  int64
	LastSeenAt int64
	Revoked    bool
}

func (s *Store) ListDevices(ctx context.Context, userID int64) ([]DeviceRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, created_at, last_seen_at, revoked FROM devices WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceRow
	for rows.Next() {
		var d DeviceRow
		var revoked int
		if err := rows.Scan(&d.ID, &d.Label, &d.CreatedAt, &d.LastSeenAt, &revoked); err != nil {
			return nil, err
		}
		d.Revoked = revoked != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDevice hard-deletes a single device row.
func (s *Store) DeleteDevice(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=?`, id)
	return err
}

// DeleteRevokedDevices hard-deletes all revoked devices of a user; returns count.
func (s *Store) DeleteRevokedDevices(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE user_id=? AND revoked=1`, userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteDeliveryLogsOlderThan deletes logs created before cutoff (unix s); returns count.
func (s *Store) DeleteDeliveryLogsOlderThan(ctx context.Context, cutoff int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM delivery_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteAllDeliveryLogs clears the delivery log; returns count.
func (s *Store) DeleteAllDeliveryLogs(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM delivery_logs`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ClearAllTelegramBindings resets every telegram channel's config to empty, so
// every user must re-bind (used when the shared bot is changed/replaced). Count.
func (s *Store) ClearAllTelegramBindings(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE channels SET config='{}', updated_at=? WHERE type='telegram'`, Now())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ClearChannelBinding empties one channel's config (used by the 403 self-heal).
func (s *Store) ClearChannelBinding(ctx context.Context, id int64) error {
	return s.SetChannelConfig(ctx, id, "{}")
}

// PurgeBindCodes deletes used or expired one-time bind codes; returns count.
func (s *Store) PurgeBindCodes(ctx context.Context, now int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tg_bind_codes WHERE used=1 OR expires_at < ?`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ClearOutbox drops all pending deliveries (undelivered messages are lost); count.
func (s *Store) ClearOutbox(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM outbox`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Counts returns per-table row counts for the admin Data page.
type Counts struct {
	Users, Devices, Channels, BindCodes, Outbox, DeliveryLogs int
}

func (s *Store) Counts(ctx context.Context) Counts {
	q := func(sql string) int {
		var n int
		_ = s.db.QueryRowContext(ctx, sql).Scan(&n)
		return n
	}
	return Counts{
		Users:        q(`SELECT COUNT(*) FROM users`),
		Devices:      q(`SELECT COUNT(*) FROM devices`),
		Channels:     q(`SELECT COUNT(*) FROM channels`),
		BindCodes:    q(`SELECT COUNT(*) FROM tg_bind_codes`),
		Outbox:       q(`SELECT COUNT(*) FROM outbox`),
		DeliveryLogs: q(`SELECT COUNT(*) FROM delivery_logs`),
	}
}

func (s *Store) SetUserStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) RevokeUserDevices(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET revoked=1 WHERE user_id=?`, userID)
	return err
}

// DeleteUser removes the user and all owned rows. devices/channels/tg_bind_codes
// cascade via FK; outbox/delivery_logs have no FK, so clear them explicitly.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM outbox WHERE user_id=?`,
		`DELETE FROM delivery_logs WHERE user_id=?`,
		`DELETE FROM users WHERE id=?`, // cascades devices/channels/tg_bind_codes
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- admin: delivery logs ----

// ListDeliveryLogs returns recent logs, optionally filtered by status ("ok"/"fail")
// and/or userID (0 = any).
func (s *Store) ListDeliveryLogs(ctx context.Context, status string, userID int64, limit int) ([]DeliveryLog, error) {
	q := `SELECT id, user_id, channel_type, sender, status, detail, created_at FROM delivery_logs WHERE 1=1`
	args := []any{}
	if status == "ok" || status == "fail" {
		q += ` AND status=?`
		args = append(args, status)
	}
	if userID > 0 {
		q += ` AND user_id=?`
		args = append(args, userID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryLog
	for rows.Next() {
		var l DeliveryLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.ChannelType, &l.Sender, &l.Status, &l.Detail, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---- admin: dashboard stats ----

type Stats struct {
	Users     int
	Channels  int
	OutboxLen int
	DeliverOK int
	DeliverNG int
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	q := func(sql string) int {
		var n int
		_ = s.db.QueryRowContext(ctx, sql).Scan(&n)
		return n
	}
	st.Users = q(`SELECT COUNT(*) FROM users`)
	st.Channels = q(`SELECT COUNT(*) FROM channels`)
	st.OutboxLen = q(`SELECT COUNT(*) FROM outbox`)
	st.DeliverOK = q(`SELECT COUNT(*) FROM delivery_logs WHERE status='ok'`)
	st.DeliverNG = q(`SELECT COUNT(*) FROM delivery_logs WHERE status='fail'`)
	return st, nil
}

// ---- settings (key/value) ----

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

// BroadcastChatID resolves the global broadcast target chat id: the DB setting
// "publish_chat_id" (admin-editable) wins; fallback (env) applies when unset.
// 0 = disabled. A channel/group chat id is negative.
func (s *Store) BroadcastChatID(ctx context.Context, fallback int64) int64 {
	if v, _ := s.GetSetting(ctx, "publish_chat_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	return fallback
}
