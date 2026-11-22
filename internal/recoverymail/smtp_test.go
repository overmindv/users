package recoverymail

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/users/internal/config"
	"github.com/overmindv/users/internal/domain"
)

func TestSMTPRejectsPlaintext(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	commands := make(chan string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			commands <- ""

			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = fmt.Fprint(conn, "220 fake SMTP\r\n")
		reader := bufio.NewReader(conn)
		hello, _ := reader.ReadString('\n')
		_, _ = fmt.Fprint(conn, "250 localhost\r\n")
		start, _ := reader.ReadString('\n')
		_, _ = fmt.Fprint(conn, "502 TLS unavailable\r\n")
		commands <- hello + start
	}()
	sender := NewSMTP(config.Recovery{SMTPAddress: listener.Addr().String(), SMTPHost: "localhost", Username: "secret-user", Password: "secret-password", From: "from@example.com"})
	err = sender.Send(context.Background(), domain.RecoveryMessage{To: "to@example.com", Purpose: domain.RecoveryReset, Token: "code"})
	if err == nil {
		t.Fatal("plaintext delivery accepted")
	}
	if sent := <-commands; strings.Contains(sent, "AUTH") || strings.Contains(sent, "secret") {
		t.Fatal("credentials sent before TLS")
	}
}
