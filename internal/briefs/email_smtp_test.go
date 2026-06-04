package briefs

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTPServer speaks just enough SMTP over one side of a net.Pipe to let the
// sender complete a full conversation. It records the DATA payload so the test
// can assert on the transmitted message. STARTTLS is intentionally not
// advertised, so the sender must run with allowInsecure to reach DATA.
type fakeSMTPServer struct {
	mu       sync.Mutex
	received string
	quit     bool
}

func (f *fakeSMTPServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(s string) {
		_, _ = w.WriteString(s)
		_ = w.Flush()
	}

	write("220 fake ESMTP\r\n")
	inData := false
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if inData {
			if line == ".\r\n" {
				inData = false
				f.mu.Lock()
				f.received = data.String()
				f.mu.Unlock()
				write("250 OK\r\n")
				continue
			}
			data.WriteString(line)
			continue
		}

		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			// No STARTTLS advertised; AUTH not required.
			write("250-fake\r\n250 PIPELINING\r\n")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			write("250 OK\r\n")
		case strings.HasPrefix(cmd, "RCPT TO"):
			write("250 OK\r\n")
		case cmd == "DATA":
			inData = true
			write("354 End data with <CR><LF>.<CR><LF>\r\n")
		case cmd == "QUIT":
			f.mu.Lock()
			f.quit = true
			f.mu.Unlock()
			write("221 Bye\r\n")
			return
		default:
			write("250 OK\r\n")
		}
	}
}

func TestSMTPEmailSender_Send(t *testing.T) {
	srv := &fakeSMTPServer{}

	sender := NewSMTPEmailSender(EmailConfig{
		Host:          "mail.example.test",
		Port:          2525,
		From:          "pilot@example.test",
		AllowInsecure: true, // fake server does not advertise STARTTLS
	})
	// Inject an in-memory pipe instead of dialing a real server.
	sender.dial = func(_ context.Context, _ string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		go srv.serve(serverConn)
		return clientConn, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := sender.Send(ctx, []string{"alice@example.test"}, "Daily Brief", "<h1>hello</h1>")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	srv.mu.Lock()
	got := srv.received
	quit := srv.quit
	srv.mu.Unlock()

	if !quit {
		t.Error("expected QUIT to be received")
	}
	for _, want := range []string{
		"From: pilot@example.test",
		"To: alice@example.test",
		"Subject: Daily Brief",
		"Content-Type: text/html",
		"<h1>hello</h1>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("transmitted message missing %q\n---\n%s", want, got)
		}
	}
}

func TestSMTPEmailSender_RefusesCleartextWithoutOptIn(t *testing.T) {
	srv := &fakeSMTPServer{}

	sender := NewSMTPEmailSender(EmailConfig{
		Host: "mail.example.test",
		Port: 2525,
		From: "pilot@example.test",
		// AllowInsecure not set → must refuse since fake advertises no STARTTLS.
	})
	sender.dial = func(_ context.Context, _ string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		go srv.serve(serverConn)
		return clientConn, nil
	}

	err := sender.Send(context.Background(), []string{"alice@example.test"}, "s", "<p>b</p>")
	if err == nil {
		t.Fatal("expected error refusing cleartext, got nil")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("expected STARTTLS refusal, got: %v", err)
	}
}

func TestNewDeliveryService_SelfWiresEmailSenderFromConfig(t *testing.T) {
	cfg := &BriefConfig{
		Email: EmailConfig{Host: "mail.example.test", From: "pilot@example.test"},
	}
	d := NewDeliveryService(cfg)
	if d.emailSender == nil {
		t.Fatal("expected email sender to be self-wired from config")
	}
	if _, ok := d.emailSender.(*SMTPEmailSender); !ok {
		t.Errorf("expected *SMTPEmailSender, got %T", d.emailSender)
	}
}

func TestNewDeliveryService_NoEmailConfig_NoSender(t *testing.T) {
	d := NewDeliveryService(&BriefConfig{})
	if d.emailSender != nil {
		t.Error("expected no email sender without config")
	}
}

func TestNewDeliveryService_ExplicitSenderWins(t *testing.T) {
	explicit := &SMTPEmailSender{host: "explicit"}
	cfg := &BriefConfig{Email: EmailConfig{Host: "from-config", From: "pilot@example.test"}}
	d := NewDeliveryService(cfg, WithEmailSender(explicit))
	got, ok := d.emailSender.(*SMTPEmailSender)
	if !ok || got.host != "explicit" {
		t.Errorf("explicit sender should win over config self-wire, got %#v", d.emailSender)
	}
}
