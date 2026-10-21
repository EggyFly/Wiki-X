package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

type Config struct {
	Host        string
	Port        int
	Username    string
	Password    string
	From        string
	ImplicitTLS bool
}

type SMTP struct {
	cfg Config
}

func NewSMTP(cfg Config) *SMTP {
	return &SMTP{cfg: cfg}
}

func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	recipient, err := mail.ParseAddress(to)
	if err != nil || recipient.Address != to || strings.ContainsAny(to, "\r\n") {
		return errors.New("invalid recipient")
	}
	from, err := mail.ParseAddress(s.cfg.From)
	if err != nil {
		return errors.New("invalid sender")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return errors.New("invalid subject")
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	conn, err := s.dial(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()
	if !s.cfg.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		} else if s.cfg.Username != "" {
			return errors.New("smtp server does not support starttls")
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(buildMessage(from.Address, recipient.Address, subject, body)); err != nil {
		writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

func (s *SMTP) dial(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if s.cfg.ImplicitTLS {
		return tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func buildMessage(from, to, subject, body string) []byte {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	var builder strings.Builder
	builder.WriteString("From: " + from + "\r\n")
	builder.WriteString("To: " + to + "\r\n")
	builder.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
	builder.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(body)
	if !strings.HasSuffix(body, "\r\n") {
		builder.WriteString("\r\n")
	}
	return []byte(builder.String())
}
