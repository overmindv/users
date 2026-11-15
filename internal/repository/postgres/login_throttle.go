package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoginThrottler разделяет TTL-счётчики всех реплик в PostgreSQL.
type LoginThrottler struct {
	pool                *pgxpool.Pool
	secret              []byte
	emailLimit, ipLimit int
	window              time.Duration
}

// NewLoginThrottler получает проверенные настройки; секрет общий для всех реплик.
func NewLoginThrottler(pool *pgxpool.Pool, secret string, emailLimit, ipLimit int, window time.Duration) *LoginThrottler {
	return &LoginThrottler{pool: pool, secret: []byte(secret), emailLimit: emailLimit, ipLimit: ipLimit, window: window}
}

func (t *LoginThrottler) digest(scope, value string) string {
	mac := hmac.New(sha256.New, t.secret)
	_, _ = mac.Write([]byte("login-throttle-v1\n" + scope + "\n" + value))
	return hex.EncodeToString(mac.Sum(nil))
}

// Check атомарно учитывает оба лимита. Отказ тоже учитывается в пределах окна.
func (t *LoginThrottler) Check(ctx context.Context, email, ip string) (bool, error) {
	if addr, err := netip.ParseAddr(ip); err == nil {
		ip = addr.Unmap().WithZone("").String()
	} else {
		ip = "unknown"
	}
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin login throttle: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	allowed := true
	for _, bucket := range []struct {
		scope, key string
		limit      int
	}{
		{"ip", ip, t.ipLimit},
		{"email", strings.ToLower(strings.TrimSpace(email)), t.emailLimit},
	} {
		var attempts int
		err = tx.QueryRow(ctx, `
   INSERT INTO login_throttle_buckets (scope, key_hash, attempts, expires_at)
   VALUES ($1, $2, 1, statement_timestamp() + $3 * interval '1 millisecond')
   ON CONFLICT (scope, key_hash) DO UPDATE SET
    attempts = CASE WHEN login_throttle_buckets.expires_at <= statement_timestamp() THEN 1
      ELSE LEAST(login_throttle_buckets.attempts + 1, $4 + 1) END,
    expires_at = CASE WHEN login_throttle_buckets.expires_at <= statement_timestamp()
      THEN statement_timestamp() + $3 * interval '1 millisecond' ELSE login_throttle_buckets.expires_at END
   RETURNING attempts`, bucket.scope, t.digest(bucket.scope, bucket.key), t.window.Milliseconds(), bucket.limit).Scan(&attempts)
		if err != nil {
			return false, fmt.Errorf("consume login throttle: %w", err)
		}
		allowed = attempts <= bucket.limit
		// Заблокированный IP не создаёт новые email buckets и не расходует чужую квоту.
		if !allowed {
			break
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit login throttle: %w", err)
	}
	return allowed, nil
}

// ResetEmail сбрасывает только email: успешный вход не обнуляет ограничение IP.
func (t *LoginThrottler) ResetEmail(ctx context.Context, email string) error {
	_, err := t.pool.Exec(ctx, `DELETE FROM login_throttle_buckets WHERE scope='email' AND key_hash=$1`,
		t.digest("email", strings.ToLower(strings.TrimSpace(email))))
	if err != nil {
		return fmt.Errorf("reset login throttle: %w", err)
	}
	return nil
}

// Cleanup удаляет ограниченный пакет, не блокируя активные запросы и другие реплики.
func (t *LoginThrottler) Cleanup(ctx context.Context) error {
	_, err := t.pool.Exec(ctx, `DELETE FROM login_throttle_buckets WHERE (scope, key_hash) IN (
  SELECT scope, key_hash FROM login_throttle_buckets WHERE expires_at <= statement_timestamp()
  ORDER BY expires_at LIMIT 1000 FOR UPDATE SKIP LOCKED
 )`)
	if err != nil {
		return fmt.Errorf("cleanup login throttle: %w", err)
	}
	return nil
}
