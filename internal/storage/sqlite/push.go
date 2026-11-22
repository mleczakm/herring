package sqlite

import (
	"context"
	"fmt"
	"time"
)

type PushSubscription struct{ Endpoint, P256DH, Auth string }

// InitPush creates the optional notification table without changing existing data.
func (s *Store) InitPush(ctx context.Context) error {
	_, err := s.database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS push_subscriptions (endpoint TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at TEXT NOT NULL) STRICT`)
	if err != nil {
		return err
	}
	_, err = s.database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS push_vapid (id INTEGER PRIMARY KEY CHECK(id=1), private_key TEXT NOT NULL, public_key TEXT NOT NULL) STRICT`)
	return err
}
func (s *Store) SavePushSubscription(ctx context.Context, userID int64, sub PushSubscription) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO push_subscriptions (endpoint,user_id,p256dh,auth,created_at) VALUES (?,?,?,?,?) ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id,p256dh=excluded.p256dh,auth=excluded.auth,created_at=excluded.created_at`, sub.Endpoint, userID, sub.P256DH, sub.Auth, formatTime(time.Now()))
	return err
}
func (s *Store) DeletePushSubscription(ctx context.Context, userID int64, endpoint string) error {
	_, err := s.database.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id=? AND endpoint=?`, userID, endpoint)
	return err
}
func (s *Store) PushSubscriptions(ctx context.Context, userID int64) ([]PushSubscription, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT endpoint,p256dh,auth FROM push_subscriptions WHERE user_id=? ORDER BY created_at LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PushSubscription
	for rows.Next() {
		var sub PushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.P256DH, &sub.Auth); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read push subscriptions: %w", err)
	}
	return result, nil
}

// EnsureVAPIDKeys stores the first generated pair atomically across processes.
func (s *Store) EnsureVAPIDKeys(ctx context.Context, privateKey, publicKey string) (string, string, error) {
	_, err := s.database.ExecContext(ctx, `INSERT OR IGNORE INTO push_vapid(id,private_key,public_key) VALUES(1,?,?)`, privateKey, publicKey)
	if err != nil {
		return "", "", err
	}
	var priv, pub string
	err = s.database.QueryRowContext(ctx, `SELECT private_key,public_key FROM push_vapid WHERE id=1`).Scan(&priv, &pub)
	return priv, pub, err
}
