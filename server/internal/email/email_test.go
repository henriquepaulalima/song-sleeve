package email

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	for _, loc := range []string{"en", "pt-BR"} {
		for _, kind := range []string{"password_reset", "password_changed"} {
			subject, text, ok := Render(kind, loc, "https://app/reset#token=T")
			if !ok || subject == "" || text == "" {
				t.Fatalf("%s/%s missing", kind, loc)
			}
			if kind == "password_reset" && !strings.Contains(text, "https://app/reset#token=T") {
				t.Fatalf("%s: no link", loc)
			}
			if strings.Contains(text, "%!") {
				t.Fatalf("%s/%s: formatting error", kind, loc)
			}
		}
	}
	if s, _, _ := Render("password_changed", "pt-BR", ""); s != "Sua senha foi alterada" {
		t.Fatalf("pt-BR subject: %q", s)
	}
	if s, _, ok := Render("password_changed", "fr", ""); !ok || s != "Your password was changed" {
		t.Fatal("unknown locales fall back to English")
	}
	if _, _, ok := Render("nope", "en", ""); ok {
		t.Fatal("unknown kind rendered")
	}
}
