// Package email sends transactional email in the account's language (D-14):
// SMTP to Mailpit in development, Resend's HTTP API in staging and production (D-13).
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/henriquepaulalima/song-sleeve/server/internal/config"
)

type Message struct {
	To      string
	Subject string
	Text    string
	// IdempotencyKey stops a retried job from sending twice through the provider.
	IdempotencyKey string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

func NewSender(cfg *config.Config) Sender {
	if cfg.ResendAPIKey != "" {
		return &resend{key: cfg.ResendAPIKey, from: cfg.EmailFrom, client: &http.Client{Timeout: 15 * time.Second}}
	}
	return &smtpSender{addr: cfg.SMTPAddr, from: cfg.EmailFrom}
}

type smtpSender struct{ addr, from string }

func (s *smtpSender) Send(_ context.Context, m Message) error {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n", s.from, m.To, m.Subject)
	if m.IdempotencyKey != "" {
		fmt.Fprintf(&b, "Message-ID: <%s@songsleeve.local>\r\n", m.IdempotencyKey)
	}
	b.WriteString("\r\n" + strings.ReplaceAll(m.Text, "\n", "\r\n"))
	from := s.from
	if i := strings.Index(from, "<"); i >= 0 {
		from = strings.Trim(from[i:], "<>")
	}
	return smtp.SendMail(s.addr, nil, from, []string{m.To}, []byte(b.String()))
}

type resend struct {
	key, from string
	client    *http.Client
}

func (r *resend) Send(ctx context.Context, m Message) error {
	body, _ := json.Marshal(map[string]any{"from": r.from, "to": []string{m.To}, "subject": m.Subject, "text": m.Text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.key)
	req.Header.Set("Content-Type", "application/json")
	if m.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", m.IdempotencyKey)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend: status %d", resp.StatusCode)
	}
	return nil
}

// Templates in both shipped locales.
var templates = map[string]map[string][2]string{
	"password_reset": {
		"en": {"Reset your Song Sleeve password",
			"Someone asked to reset the password for your Song Sleeve account.\n\nSet a new password here:\n%s\n\nThe link works once, for 30 minutes. Setting a new password signs you out on every device.\n\nIf you didn't ask for this, you can ignore this email."},
		"pt-BR": {"Redefina sua senha do Song Sleeve",
			"Alguém pediu para redefinir a senha da sua conta do Song Sleeve.\n\nCrie uma nova senha aqui:\n%s\n\nO link funciona uma vez, por 30 minutos. Criar uma nova senha encerra a sessão em todos os dispositivos.\n\nSe você não pediu isso, pode ignorar este e-mail."},
	},
	"password_changed": {
		"en": {"Your password was changed",
			"The password for your Song Sleeve account was just changed. Your other devices were signed out.\n\nIf this wasn't you, reset your password right away from the Song Sleeve app."},
		"pt-BR": {"Sua senha foi alterada",
			"A senha da sua conta do Song Sleeve acabou de ser alterada. Seus outros dispositivos foram desconectados.\n\nSe não foi você, redefina sua senha agora mesmo pelo app Song Sleeve."},
	},
}

// Render builds the subject and text for kind in locale.
func Render(kind, locale, link string) (subject, text string, ok bool) {
	t, ok := templates[kind]
	if !ok {
		return "", "", false
	}
	tpl, ok := t[locale]
	if !ok {
		tpl = t["en"]
	}
	text = tpl[1]
	if strings.Contains(text, "%s") {
		text = fmt.Sprintf(text, link)
	}
	return tpl[0], text, true
}
