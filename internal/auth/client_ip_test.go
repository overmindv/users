package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestLoginClientIP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, secret, signatureSecret, ip string
		age                               int64
		want                              string
	}{
		{"valid", "shared-secret", "shared-secret", "198.51.100.9", 0, "198.51.100.9"},
		{"wrong signature", "shared-secret", "attacker", "198.51.100.9", 0, "192.0.2.1"},
		{"expired", "shared-secret", "shared-secret", "198.51.100.9", -61, "192.0.2.1"},
		{"future", "shared-secret", "shared-secret", "198.51.100.9", 60, "192.0.2.1"},
		{"invalid ip", "shared-secret", "shared-secret", "forged", 0, "192.0.2.1"},
		{"no secret", "", "", "198.51.100.9", 0, "192.0.2.1"},
		{"canonical ipv4", "shared-secret", "shared-secret", "::ffff:198.51.100.9", 0, "198.51.100.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
			req.RemoteAddr = "192.0.2.1:1234"
			timestamp := strconv.FormatInt(time.Now().Unix()+tc.age, 10)
			mac := hmac.New(sha256.New, []byte(tc.signatureSecret))
			_, _ = mac.Write([]byte("login-client-ip-v1\n" + timestamp + "\n" + tc.ip))
			req.Header.Set("X-Login-Client-IP", tc.ip)
			req.Header.Set("X-Login-Client-Time", timestamp)
			req.Header.Set("X-Login-Client-Signature", hex.EncodeToString(mac.Sum(nil)))
			req.Header.Set("X-Forwarded-For", "203.0.113.9")
			req.Header.Set("Forwarded", "for=203.0.113.9")
			LoginClientIP(tc.secret, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if got := ClientIP(r.Context()); got != tc.want {
					t.Fatalf("got %s, want %s", got, tc.want)
				}
			})).ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}
