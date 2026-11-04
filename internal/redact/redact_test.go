package redact

import (
	"strings"
	"testing"
)

func TestRedactsKnownSecrets(t *testing.T) {
	cases := map[string]string{
		"token sk-proj-abcdefghijklmnopqrstuvwxyz123456":             "[REDACTED:openai]",
		"github ghp_0123456789abcdefghijABCDEFGHIJ":                  "[REDACTED:github]",
		"aws AKIAIOSFODNN7EXAMPLE":                                   "[REDACTED:aws]",
		"slack xoxb-1234567890-abcdefghij":                           "[REDACTED:slack]",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789": "[REDACTED:bearer]",
	}
	for input, want := range cases {
		if got := String(input); got == input || !strings.Contains(got, want) {
			t.Fatalf("String(%q) = %q, want marker %q", input, got, want)
		}
	}
}

func TestKeepsOrdinaryText(t *testing.T) {
	input := "部署失败时先看 Cloudflare 日志；API key 放在 1Password。"
	if got := String(input); got != input {
		t.Fatalf("ordinary text was changed: %q", got)
	}
}
