package worker

import (
	"context"
	"log/slog"
	"time"
)

type throttleCleaner interface{ Cleanup(context.Context) error }

// RunLoginThrottleCleanup ограничивает время запроса и объём каждой фоновой очистки.
func RunLoginThrottleCleanup(cleaner throttleCleaner, log *slog.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := cleaner.Cleanup(cleanupCtx)
				cancel()
				if err != nil && ctx.Err() == nil {
					log.WarnContext(ctx, "login throttle cleanup failed")
				}
			}
		}
	}
}
