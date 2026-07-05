package email

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"net/mail"
	"net/smtp"
	"strings"

	"github.com/PixelAudit/PixelAudit/internal/config"
)

type Mailer struct {
	host      string
	port      int
	user      string
	pass      string
	secure    bool
	fromEmail string
	fromName  string
}

type WelcomeData struct {
	Name string
}

func New(cfg *config.Config) *Mailer {
	return &Mailer{
		host:      cfg.SMTPHost,
		port:      cfg.SMTPPort,
		user:      cfg.SMTPUser,
		pass:      cfg.SMTPPass,
		secure:    cfg.SMTPSecure,
		fromEmail: cfg.SMTPFromEmail,
		fromName:  cfg.SMTPFromName,
	}
}

func (m *Mailer) Configured() bool {
	return m.host != "" && m.port > 0 && m.user != "" && m.pass != "" && m.fromEmail != ""
}

func (m *Mailer) SendWelcome(toEmail, toName string) error {
	if !m.Configured() {
		return nil
	}

	var body bytes.Buffer
	if err := welcomeTemplate.Execute(&body, WelcomeData{Name: firstName(toName)}); err != nil {
		return err
	}

	from := mail.Address{Name: m.fromName, Address: m.fromEmail}
	to := mail.Address{Name: toName, Address: toEmail}
	msg := bytes.Buffer{}
	fmt.Fprintf(&msg, "From: %s\r\n", from.String())
	fmt.Fprintf(&msg, "To: %s\r\n", to.String())
	fmt.Fprintf(&msg, "Subject: Bem-vindo ao PixelAudit\r\n")
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&msg, "Content-Type: text/html; charset=UTF-8\r\n")
	fmt.Fprintf(&msg, "\r\n%s", body.String())

	addr := fmt.Sprintf("%s:%d", m.host, m.port)
	auth := smtp.PlainAuth("", m.user, m.pass, m.host)
	if m.secure && m.port == 465 {
		return m.sendImplicitTLS(addr, auth, from.Address, []string{to.Address}, msg.Bytes())
	}
	return m.sendStartTLS(addr, auth, from.Address, []string{to.Address}, msg.Bytes())
}

func (m *Mailer) sendStartTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Close()

	if m.secure {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (m *Mailer) sendImplicitTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.DialWithDialer(&net.Dialer{}, "tcp", addr, &tls.Config{
		ServerName: m.host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func firstName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "there"
	}
	return strings.Fields(name)[0]
}

var welcomeTemplate = template.Must(template.New("welcome").Parse(`
<!doctype html>
<html>
  <body style="margin:0;background:#0b1020;color:#f8fafc;font-family:Inter,Arial,sans-serif;">
    <div style="max-width:560px;margin:0 auto;padding:32px 20px;">
      <div style="border:1px solid rgba(255,255,255,.12);border-radius:16px;background:#111827;padding:28px;">
        <p style="margin:0 0 12px;color:#38bdf8;font-size:12px;letter-spacing:.12em;text-transform:uppercase;">PixelAudit</p>
        <h1 style="margin:0 0 12px;font-size:26px;line-height:1.2;">Welcome, {{.Name}}.</h1>
        <p style="margin:0 0 18px;color:#cbd5e1;font-size:15px;line-height:1.6;">
          Your PixelAudit account is ready. You can upload an image and receive a clear report showing whether it looks authentic, edited, or AI-generated.
        </p>
        <p style="margin:0;color:#94a3b8;font-size:13px;line-height:1.6;">
          This first workflow is optimized for fast image checks. Enterprise API access can be enabled from the integrations area.
        </p>
      </div>
      <p style="margin:18px 0 0;color:#64748b;font-size:12px;text-align:center;">
        PixelAudit Enterprise
      </p>
    </div>
  </body>
</html>`))
