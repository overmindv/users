//go:build component

package integration

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	postgresrepo "github.com/overmindv/users/internal/repository/postgres"
)

func throttlePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN is required")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPostgresLoginThrottleReplicasAndExpiry(t *testing.T) {
	ctx := context.Background()
	pool1, pool2 := throttlePool(t), throttlePool(t)
	clear := func() {
		t.Helper()
		if _, err := pool1.Exec(ctx, "TRUNCATE login_throttle_buckets"); err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []string{"email", "ip"} {
		t.Run(scope, func(t *testing.T) {
			clear()
			replicas := []*postgresrepo.LoginThrottler{
				postgresrepo.NewLoginThrottler(pool1, "test-key", 5, 7, time.Minute),
				postgresrepo.NewLoginThrottler(pool2, "test-key", 5, 7, time.Minute),
			}
			var allowed atomic.Int64
			var wg sync.WaitGroup
			for i := 0; i < 40; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					email, ip := "limited@example.com", fmt.Sprintf("192.0.2.%d", i+1)
					if scope == "ip" {
						email, ip = fmt.Sprintf("user%d@example.com", i), "192.0.2.100"
					}
					ok, err := replicas[i%2].Check(ctx, email, ip)
					if err != nil {
						t.Errorf("check: %v", err)
						return
					}
					if ok {
						allowed.Add(1)
					}
				}(i)
			}
			wg.Wait()
			want := int64(5)
			email, ip := " LIMITED@EXAMPLE.COM ", "192.0.2.100"
			if scope == "ip" {
				want, email = 7, "new@example.com"
			}
			if allowed.Load() != want {
				t.Fatalf("allowed %d, want %d", allowed.Load(), want)
			}
			restarted := postgresrepo.NewLoginThrottler(pool2, "test-key", 5, 7, time.Minute)
			if ok, err := restarted.Check(ctx, email, ip); err != nil || ok {
				t.Fatalf("restart bypass: %v %v", ok, err)
			}
			var plaintext int
			if err := pool1.QueryRow(ctx, "SELECT count(*) FROM login_throttle_buckets WHERE key_hash !~ '^[0-9a-f]{64}$'").Scan(&plaintext); err != nil || plaintext != 0 {
				t.Fatalf("plaintext: %d %v", plaintext, err)
			}
			// Переводим только тестовые окна в прошлое; не ждём реального TTL.
			if _, err := pool1.Exec(ctx, "UPDATE login_throttle_buckets SET expires_at=clock_timestamp()-interval '1 second'"); err != nil {
				t.Fatal(err)
			}
			if ok, err := restarted.Check(ctx, email, ip); err != nil || !ok {
				t.Fatalf("expiry: %v %v", ok, err)
			}
			if err := restarted.Cleanup(ctx); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := pool1.QueryRow(ctx, "SELECT count(*) FROM login_throttle_buckets").Scan(&count); err != nil || count != 2 {
				t.Fatalf("cleanup removed active buckets: %d %v", count, err)
			}
		})
	}
}

func TestPostgresLoginThrottleResetPreservesIPAndCleanupBound(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	if _, err := pool.Exec(ctx, "TRUNCATE login_throttle_buckets"); err != nil {
		t.Fatal(err)
	}
	throttle := postgresrepo.NewLoginThrottler(pool, "test-key", 2, 2, time.Minute)
	for i := 0; i < 2; i++ {
		if ok, err := throttle.Check(ctx, "user@example.com", "::ffff:192.0.2.1"); err != nil || !ok {
			t.Fatalf("allow: %v %v", ok, err)
		}
	}
	if err := throttle.ResetEmail(ctx, " USER@EXAMPLE.COM "); err != nil {
		t.Fatal(err)
	}
	if ok, err := throttle.Check(ctx, "user@example.com", "192.0.2.1"); err != nil || ok {
		t.Fatalf("IP reset bypass: %v %v", ok, err)
	}
	if ok, err := throttle.Check(ctx, "user@example.com", "192.0.2.2"); err != nil || !ok {
		t.Fatalf("email reset: %v %v", ok, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO login_throttle_buckets SELECT 'ip', lpad(i::text,64,'0'), 1, clock_timestamp()-interval '1 second' FROM generate_series(1,1005) i`); err != nil {
		t.Fatal(err)
	}
	if err := throttle.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM login_throttle_buckets WHERE expires_at <= clock_timestamp()").Scan(&count); err != nil || count != 5 {
		t.Fatalf("cleanup bound: %d %v", count, err)
	}
	if err := throttle.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if ok, err := throttle.Check(ctx, "any@example.com", "192.0.2.1"); err == nil || ok {
		t.Fatalf("DB error must fail closed: %v %v", ok, err)
	}
}

func TestPostgresBlockedIPDoesNotConsumeEmailBuckets(t *testing.T) {
	ctx := context.Background()
	pool := throttlePool(t)
	if _, err := pool.Exec(ctx, "TRUNCATE login_throttle_buckets"); err != nil {
		t.Fatal(err)
	}
	throttle := postgresrepo.NewLoginThrottler(pool, "test-key", 5, 2, time.Minute)
	for i := 0; i < 2; i++ {
		if ok, err := throttle.Check(ctx, "user@example.com", "192.0.2.1"); err != nil || !ok {
			t.Fatalf("allow: %v %v", ok, err)
		}
	}
	for i := 0; i < 50; i++ {
		if ok, err := throttle.Check(ctx, fmt.Sprintf("blocked%d@example.com", i), "192.0.2.1"); err != nil || ok {
			t.Fatalf("blocked: %v %v", ok, err)
		}
	}
	if ok, err := throttle.Check(ctx, "user@example.com", "192.0.2.1"); err != nil || ok {
		t.Fatalf("existing email: %v %v", ok, err)
	}
	var count, attempts int
	if err := pool.QueryRow(ctx, "SELECT count(*), sum(attempts) FROM login_throttle_buckets WHERE scope='email'").Scan(&count, &attempts); err != nil || count != 1 || attempts != 2 {
		t.Fatalf("blocked IP consumed email: %d %d %v", count, attempts, err)
	}
}
