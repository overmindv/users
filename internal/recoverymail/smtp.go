package recoverymail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/overmindv/users/internal/config"
	"github.com/overmindv/users/internal/domain"
)

type SMTP struct{ config config.Recovery }

func NewSMTP(c config.Recovery) *SMTP { return &SMTP{config: c} }

// Send использует STARTTLS до аутентификации; незашифрованный SMTP запрещён.
func (s *SMTP) Send(ctx context.Context, message domain.RecoveryMessage) error {
	to, err := mail.ParseAddress(message.To)
	if err != nil || to.Address != message.To || strings.ContainsAny(message.To, "\r\n") ||
		strings.ContainsAny(message.Token, "\r\n") || (message.Purpose != domain.RecoveryReset && message.Purpose != domain.RecoveryVerify) {
		return errors.New("invalid recovery message")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.config.SMTPAddress)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	client, err := smtp.NewClient(conn, s.config.SMTPHost)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if err = client.StartTLS(&tls.Config{ServerName: s.config.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	if err = client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.SMTPHost)); err != nil {
		return err
	}
	if err = client.Mail(s.config.From); err != nil {
		return err
	}
	if err = client.Rcpt(message.To); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	action, ttl := "восстановления доступа", "30 минут"
	if message.Purpose == domain.RecoveryVerify {
		action, ttl = "подтверждения email", "24 часа"
	}
	_, err = fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nSubject: Overmindv account confirmation\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nКод для %s:\r\n%s\r\n\r\nКод действует %s с момента запроса. Если вы не запрашивали письмо, проигнорируйте его.\r\n", s.config.From, message.To, action, message.Token, ttl)
	if err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}

	return client.Quit()
}
