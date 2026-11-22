package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	graphqldelivery "github.com/overmindv/users/internal/delivery/graphql"
	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/security"
	"github.com/overmindv/users/internal/usecase"
)

type recoveryGraphStore struct{}

func (recoveryGraphStore) QueueRecovery(context.Context, string, string, []byte, []byte) error {
	return nil
}
func (recoveryGraphStore) ConsumeRecovery(context.Context, []byte, string, string) error {
	return domain.ErrInvalidRecoveryToken
}

type recoveryGraphCipher struct{}

func (recoveryGraphCipher) Seal(domain.RecoveryMessage) ([]byte, error) { return []byte("sealed"), nil }

type recoveryGraphThrottle struct{}

func (recoveryGraphThrottle) Check(context.Context, string, string) (bool, error) { return true, nil }

func TestRecoveryGraphQL(t *testing.T) {
	t.Parallel()
	service := usecase.NewRecovery(recoveryGraphStore{}, recoveryGraphCipher{}, security.PlainTextHasher{}, recoveryGraphThrottle{}, recoveryGraphThrottle{})
	mux := http.NewServeMux()
	graphqldelivery.Register(mux, &graphqldelivery.Resolver{Recovery: service}, func(h http.Handler) http.Handler { return h }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, address := range []string{"existing@example.com", "unknown@example.com"} {
		response := postGraphQL(t, mux, "", `mutation($email:String!){requestAccountRecovery(email:$email)}`, map[string]any{"email": address})
		if len(response.Errors) > 0 || string(response.Data) != `{"requestAccountRecovery":true}` {
			t.Fatal(response)
		}
	}
	response := postGraphQL(t, mux, "", `mutation{resetAccountPassword(token:"bad",password:"new-password")}`, nil)
	if len(response.Errors) == 0 {
		t.Fatal("bad reset accepted")
	}
	response = postGraphQL(t, mux, "", `mutation{verifyAccountEmail(token:"bad")}`, nil)
	if len(response.Errors) == 0 {
		t.Fatal("bad verify accepted")
	}
}
