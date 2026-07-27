package store

import (
	"context"
	"database/sql"
)

// ---- users ----

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (*User, error) {
	now := Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username, password_hash, status, created_at) VALUES(?,?,'active',?)`,
		username, passwordHash, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, PasswordHash: passwordHash, Status: "active", CreatedAt: now}, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, status, created_at FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Status, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

// ---- devices ----

func (s *Store) CreateDevice(ctx context.Context, userID int64, tokenHash, label string) (*Device, error) {
	now := Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO devices(user_id, token_hash, label, created_at, last_seen_at) VALUES(?,?,?,?,?)`,
		userID, tokenHash, label, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Device{ID: id, UserID: userID, TokenHash: tokenHash, Label: label, CreatedAt: now}, nil
}

// UserByDeviceToken resolves an active device token hash to its (active) user,
// and bumps last_seen_at. Returns ErrNotFound if the token is unknown/revoked
// or the user is not active.
func (s *Store) UserByDeviceToken(ctx context.Context, tokenHash string) (*User, int64, error) {
	var u User
	var deviceID int64
	err := s.db.QueryRowContext(ctx, `
SELECT u.id, u.username, u.password_hash, u.status, u.created_at, d.id
FROM devices d JOIN users u ON u.id = d.user_id
WHERE d.token_hash=? AND d.revoked=0 AND u.status='active'`, tokenHash).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Status, &u.CreatedAt, &deviceID)
	if err == sql.ErrNoRows {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE devices SET last_seen_at=? WHERE id=?`, Now(), deviceID)
	return &u, deviceID, nil
}

func (s *Store) RevokeDevice(ctx context.Context, deviceID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET revoked=1 WHERE id=?`, deviceID)
	return err
}

// ---- channels ----

func (s *Store) ListChannels(ctx context.Context, userID int64) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, type, enabled, config, created_at, updated_at
		 FROM channels WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetChannel returns a channel only if it belongs to userID.
func (s *Store) GetChannel(ctx context.Context, userID, id int64) (*Channel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, type, enabled, config, created_at, updated_at
		 FROM channels WHERE id=? AND user_id=?`, id, userID)
	c, err := scanChannel(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateChannel(ctx context.Context, userID int64, typ string, enabled bool, config string) (*Channel, error) {
	now := Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO channels(user_id, type, enabled, config, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		userID, typ, b2i(enabled), config, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Channel{ID: id, UserID: userID, Type: typ, Enabled: enabled, Config: config, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) UpdateChannel(ctx context.Context, userID, id int64, enabled bool, config string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE channels SET enabled=?, config=?, updated_at=? WHERE id=? AND user_id=?`,
		b2i(enabled), config, Now(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetChannelConfig overwrites just the config JSON (used by the TG bind flow).
func (s *Store) SetChannelConfig(ctx context.Context, id int64, config string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET config=?, updated_at=? WHERE id=?`, config, Now(), id)
	return err
}

func (s *Store) DeleteChannel(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface{ Scan(dest ...any) error }

func scanChannel(sc scanner) (*Channel, error) {
	var c Channel
	var enabled int
	if err := sc.Scan(&c.ID, &c.UserID, &c.Type, &enabled, &c.Config, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.Enabled = enabled != 0
	return &c, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
