//go:build component

package integration

import (
	"context"
	"testing"
	"time"
)

func TestMigrationsSkipDemoUsers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := throttlePool(t)
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id BETWEEN '00000000-0000-0000-0000-000000000001'::uuid
			             AND '00000000-0000-0000-0000-000000000010'::uuid
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("legacy demo accounts in the migration test database")
	}
}
