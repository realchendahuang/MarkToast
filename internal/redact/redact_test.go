package redact

import (
	"strings"
	"testing"
)

func TestRedactsKnownSecrets(t *testing.T) {
	cases := map[string]string{
		"token sk-proj-abcdefghijklmnopqrstuvwxyz123456":                                                        "[REDACTED:openai]",
		"anthropic sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789":                                           "[REDACTED:anthropic]",
		"github ghp_0123456789abcdefghijABCDEFGHIJ":                                                             "[REDACTED:github]",
		"fine-grained github_pat_11ABCDEFG0123456789abcdefghij":                                                 "[REDACTED:github-pat]",
		"gitlab glpat-0123456789abcdefghijABCD":                                                                 "[REDACTED:gitlab]",
		"aws AKIAIOSFODNN7EXAMPLE":                                                                              "[REDACTED:aws]",
		`aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`:                                    "[REDACTED:aws-secret]",
		"google AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe":                                                        "[REDACTED:google-api]",
		"oauth ya29.a0AfH6SMBx1234567890abcdefghijklmnop":                                                       "[REDACTED:google-oauth]",
		"hf hf_abcdefghijklmnopqrstuvwxyz0123456789":                                                            "[REDACTED:huggingface]",
		"npm npm_abcdefghijklmnopqrstuvwxyz0123456789":                                                          "[REDACTED:npm]",
		"slack xoxb-1234567890-abcdefghij":                                                                      "[REDACTED:slack]",
		"webhook https://hooks.slack.com/services/T12345678/" + "B12345678/abcdefghijklmnopqrstuvwx":            "[REDACTED:slack]",
		"stripe sk_live_" + "0123456789abcdefghijklmn":                                                          "[REDACTED:stripe]",
		"jwt eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6y": "[REDACTED:jwt]",
		"dsn postgres://admin:s3cr3tP4ssw0rd99@db.internal:5432/app":                                            "postgres://admin:[REDACTED:url-auth]@db.internal:5432/app",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789":                                            "[REDACTED:bearer]",
		"-----BEGIN ENCRYPTED PRIVATE KEY-----\nabcdef\n-----END ENCRYPTED PRIVATE KEY-----":                    "[REDACTED:private-key]",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----\nabcdef\n-----END PGP PRIVATE KEY BLOCK-----":                    "[REDACTED:private-key]",
	}
	for input, want := range cases {
		if got := String(input); got == input || !strings.Contains(got, want) {
			t.Fatalf("String(%q) = %q, want marker %q", input, got, want)
		}
	}
}

func TestKeepsOrdinaryTextAndPlaceholders(t *testing.T) {
	cases := []string{
		"部署失败时先看 Cloudflare 日志；API key 放在 1Password。",
		"Configure OPENAI_API_KEY=sk-xxxxxxxxxxxxxxxxxxxxxxxx in your local .env file.",
		"Set Authorization: Bearer YOUR_API_TOKEN_PLACEHOLDER_HERE when testing.",
		"The standard bearer /usr/local/share/documentation/handbook.md explains the flow.",
		"Example DSN: postgres://user:password@localhost:5432/mydb or postgres://user:${DB_PASS}@localhost/db",
		"Normal identifiers like sk_custom_helper or v0.1.0-release-candidate should remain untouched.",
	}
	for _, input := range cases {
		if got := String(input); got != input {
			t.Fatalf("ordinary text or placeholder was changed:\n in: %q\ngot: %q", input, got)
		}
	}
}

func TestRulesNonEmptyAndUnique(t *testing.T) {
	names := Rules()
	if len(names) < 10 {
		t.Fatalf("expected comprehensive rule list, got %d", len(names))
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			t.Fatalf("duplicate or empty rule name %q in %v", name, names)
		}
		seen[name] = true
	}
}
