package briefs

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPEmailSender delivers HTML briefs over SMTP. It satisfies EmailSender and
// is constructible from a BriefConfig's EmailConfig, so the wiring layer can
// attach it via WithEmailSender when email channels are configured.
type SMTPEmailSender struct {
	host     string
	port     int
	from     string
	username string
	password string

	// allowInsecure permits delivery to a server that does not advertise
	// STARTTLS. Off by default so credentials and brief bodies are never sent in
	// cleartext unless an operator opts in for a trusted internal relay.
	allowInsecure bool
	// tlsConfig is used for the STARTTLS upgrade; defaults to verifying the
	// server certificate against host. Overridable for tests.
	tlsConfig *tls.Config

	// dial connects to the SMTP server. Overridable in tests so the message-build
	// and conversation logic can run against an in-memory server.
	dial func(ctx context.Context, addr string) (net.Conn, error)
}

// NewSMTPEmailSender builds an SMTP sender from email configuration.
func NewSMTPEmailSender(cfg EmailConfig) *SMTPEmailSender {
	port := cfg.Port
	if port == 0 {
		port = 587 // STARTTLS submission default
	}
	return &SMTPEmailSender{
		host:          cfg.Host,
		port:          port,
		from:          cfg.From,
		username:      cfg.Username,
		password:      cfg.Password,
		allowInsecure: cfg.AllowInsecure,
		tlsConfig:     &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12},
		dial: func(ctx context.Context, addr string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "tcp", addr)
		},
	}
}

// SetTLSConfig overrides the STARTTLS client config (used in tests).
func (s *SMTPEmailSender) SetTLSConfig(cfg *tls.Config) { s.tlsConfig = cfg }

// buildMessage assembles the MIME message bytes for the email.
func (s *SMTPEmailSender) buildMessage(to []string, subject, htmlBody string) string {
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", s.from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ",")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)
	return msg.String()
}

// Send delivers an HTML email via SMTP. It honors ctx (so a wedged mail server
// cannot block delivery indefinitely) and prefers STARTTLS, refusing cleartext
// transmission unless allowInsecure is set.
func (s *SMTPEmailSender) Send(ctx context.Context, to []string, subject, htmlBody string) error {
	if len(to) == 0 {
		return fmt.Errorf("smtp: no recipients")
	}
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	body := s.buildMessage(to, subject, htmlBody)

	conn, err := s.dial(ctx, addr)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		cfg := s.tlsConfig
		if cfg == nil {
			cfg = &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
		}
		if err := client.StartTLS(cfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	} else if !s.allowInsecure {
		return fmt.Errorf("smtp server %s does not advertise STARTTLS; refusing to send in cleartext (set allow_insecure for trusted relays)", s.host)
	}

	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("smtp mail from %s: %w", s.from, err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}

	return client.Quit()
}
