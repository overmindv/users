package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

type clientIPKey struct{}

// ClientIP возвращает IP, проверенный HTTP-границей, а не значение из GraphQL input.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)

	return ip
}

// LoginClientIP принимает подписанный контекст gateway; иначе использует TCP peer.
// Пользовательские XFF/Forwarded никогда не считаются источником доверия.
func LoginClientIP(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		peer, err := netip.ParseAddr(host)
		ip := "unknown"
		if err == nil {
			ip = peer.Unmap().WithZone("").String()
		}
		forwarded := r.Header.Get("X-Login-Client-IP")
		timestamp := r.Header.Get("X-Login-Client-Time")
		signature, sigErr := hex.DecodeString(r.Header.Get("X-Login-Client-Signature"))
		issued, timeErr := strconv.ParseInt(timestamp, 10, 64)
		addr, ipErr := netip.ParseAddr(forwarded)
		now := time.Now().Unix()
		if secret != "" && sigErr == nil && timeErr == nil && ipErr == nil && issued >= now-60 && issued <= now+5 {
			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = mac.Write([]byte("login-client-ip-v1\n" + timestamp + "\n" + forwarded))
			if hmac.Equal(signature, mac.Sum(nil)) {
				ip = addr.Unmap().WithZone("").String()
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
	})
}
