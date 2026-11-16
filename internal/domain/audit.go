package domain

import "context"

type auditActorKey struct{}

// WithAuditActor передаёт подтверждённого actor в транзакцию аудита.
func WithAuditActor(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, auditActorKey{}, id)
}

// AuditActor возвращает actor; пустое значение обозначает системную операцию.
func AuditActor(ctx context.Context) string { id, _ := ctx.Value(auditActorKey{}).(string); return id }
