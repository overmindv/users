package config

import (
	"encoding/base64"
	"errors"
	"net"
	"net/mail"
	"os"
	"strconv"
	"strings"
)

type Recovery struct {
	Enabled                                         bool
	SMTPAddress, SMTPHost, Username, Password, From string
	EncryptionKey                                   []byte
}

func loadRecovery() (Recovery, error) {
	c := Recovery{SMTPAddress: os.Getenv("USERS_RECOVERY_SMTP_ADDRESS"), Username: os.Getenv("USERS_RECOVERY_SMTP_USERNAME"), Password: os.Getenv("USERS_RECOVERY_SMTP_PASSWORD"), From: os.Getenv("USERS_RECOVERY_FROM")}
	key := os.Getenv("USERS_RECOVERY_MAIL_KEY")
	if c.SMTPAddress == "" && c.Username == "" && c.Password == "" && c.From == "" && key == "" {
		return c, nil
	}
	host, port, err := net.SplitHostPort(c.SMTPAddress)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 || c.Username == "" || c.Password == "" {
		return Recovery{}, errors.New("invalid recovery SMTP configuration")
	}
	from, err := mail.ParseAddress(c.From)
	if err != nil || from.Address != c.From || strings.ContainsAny(c.From, "\r\n") {
		return Recovery{}, errors.New("invalid recovery sender")
	}
	c.EncryptionKey, err = base64.StdEncoding.DecodeString(key)
	if err != nil || len(c.EncryptionKey) != 32 {
		return Recovery{}, errors.New("invalid recovery mail encryption key")
	}
	c.SMTPHost, c.Enabled = host, true

	return c, nil
}
