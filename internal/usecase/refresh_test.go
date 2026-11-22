package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/security"
)

type refreshTokensStub struct {
	fakeTokens
	user    *domain.User
	issued  bool
	revoked bool
	failure error
}

func (s *refreshTokensStub) IssueRefreshSession(_ context.Context, u *domain.User) (domain.SessionTokens, error) {
	s.issued = true
	s.user = u

	return domain.SessionTokens{UserID: u.ID(), AccessToken: "access", RefreshToken: "refresh", AccessExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour)}, s.failure
}
func (s *refreshTokensStub) RefreshSession(context.Context, string) (domain.SessionTokens, error) {
	return domain.SessionTokens{User: s.user, UserID: fakeUserID, AccessToken: "next", RefreshToken: "next-refresh"}, s.failure
}
func (s *refreshTokensStub) RevokeRefreshSession(context.Context, string) error {
	s.revoked = true

	return s.failure
}

func TestRefreshUsecase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFakeRepository()
	tokens := &refreshTokensStub{}
	svc := NewUserService(repo, security.PlainTextHasher{}, tokens, fakeIDs{}, fakeClock{time.Now()})
	result, err := svc.Register(ctx, RegisterInput{Email: "refresh@example.com", Password: "password123", Username: "refreshuser", RefreshSession: true})
	if err != nil {
		t.Fatal(err)
	}
	if !tokens.issued || result.RefreshToken != "refresh" {
		t.Fatal("opt-in not used")
	}
	result, err = svc.RefreshSession(ctx, "refresh")
	if err != nil || result.Token != "next" || result.User.ID() != fakeUserID {
		t.Fatalf("refresh: %v", err)
	}
	if err = svc.RevokeRefreshSession(ctx, "next-refresh"); err != nil || !tokens.revoked {
		t.Fatalf("logout: %v", err)
	}
	tokens.failure = domain.ErrUnauthorized
	if _, err = svc.RefreshSession(ctx, "used"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err = svc.Login(ctx, LoginInput{Email: "refresh@example.com", Password: "password123", RefreshSession: true}); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal(err)
	}
	tokens.failure = nil
	repo.err = errors.New("storage down")
	if _, err = svc.RefreshSession(ctx, "refresh"); err != nil {
		t.Fatal(err)
	}
	svc.tokens = fakeTokens{}
	if _, err = svc.RefreshSession(ctx, "refresh"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal(err)
	}
	if err = svc.RevokeRefreshSession(ctx, "refresh"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal(err)
	}
}
