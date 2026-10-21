package mail

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type smtpTestServer struct {
	listener net.Listener
	mu       sync.Mutex
	messages []string
}

func startSMTPTestServer(t *testing.T) *smtpTestServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &smtpTestServer{listener: listener}
	t.Cleanup(func() { listener.Close() })
	go server.serve()
	return server
}

func (s *smtpTestServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *smtpTestServer) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	write := func(line string) {
		writer.WriteString(line + "\r\n")
		writer.Flush()
	}
	write("220 mail.test ESMTP")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			write("250-mail.test")
			write("250 8BITMIME")
		case strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"):
			write("250 OK")
		case strings.HasPrefix(command, "DATA"):
			write("354 End data with <CR><LF>.<CR><LF>")
			var builder strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				builder.WriteString(dataLine)
			}
			s.mu.Lock()
			s.messages = append(s.messages, builder.String())
			s.mu.Unlock()
			write("250 OK")
		case strings.HasPrefix(command, "QUIT"):
			write("221 Bye")
			return
		default:
			write("250 OK")
		}
	}
}

func (s *smtpTestServer) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return ""
	}
	return s.messages[len(s.messages)-1]
}

func TestSMTPSend(t *testing.T) {
	server := startSMTPTestServer(t)
	host, portText, err := net.SplitHostPort(server.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	sender := NewSMTP(Config{
		Host: host,
		Port: port,
		From: "wiki@example.com",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sender.Send(ctx, "player@example.com", "验证你的邮箱", "请打开链接验证邮箱"); err != nil {
		t.Fatal(err)
	}
	message := server.last()
	for _, want := range []string{"From: wiki@example.com", "To: player@example.com", "请打开链接验证邮箱"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message missing %q:\n%s", want, message)
		}
	}
	if !strings.Contains(message, "Subject: ") {
		t.Fatalf("message missing subject:\n%s", message)
	}
}

func TestSMTPRejectsHeaderInjection(t *testing.T) {
	sender := NewSMTP(Config{Host: "127.0.0.1", Port: 1, From: "wiki@example.com"})
	ctx := context.Background()
	if err := sender.Send(ctx, "player@example.com\r\nBcc: x@example.com", "subject", "body"); err == nil {
		t.Fatal("recipient with crlf should be rejected")
	}
	if err := sender.Send(ctx, "not-an-email", "subject", "body"); err == nil {
		t.Fatal("invalid recipient should be rejected")
	}
}
