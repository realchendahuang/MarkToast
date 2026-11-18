// Package redact removes high-confidence secrets from memory text before it
// is written. It is deliberately conservative: patterns must match well-known
// credential shapes, so ordinary prose and example placeholders survive.
package redact

import (
	"regexp"
	"strings"
)

type rule struct {
	name    string
	re      *regexp.Regexp
	replace func(match string) string
}

var rules = []rule{
	{
		name: "private-key",
		re:   regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?-----END (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`),
	},
	{
		name:    "anthropic",
		re:      regexp.MustCompile(`\bsk-ant-(?:api\d{2}-|admin\d{2}-)?[A-Za-z0-9_-]{20,}\b`),
		replace: redactUnlessPlaceholder("anthropic", "sk-ant-"),
	},
	{
		name:    "openai",
		re:      regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}\b`),
		replace: redactUnlessPlaceholder("openai", "sk-"),
	},
	{
		name:    "github",
		re:      regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
		replace: redactUnlessPlaceholder("github", "ghp_"),
	},
	{
		name:    "github-pat",
		re:      regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
		replace: redactUnlessPlaceholder("github-pat", "github_pat_"),
	},
	{
		name:    "gitlab",
		re:      regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`),
		replace: redactUnlessPlaceholder("gitlab", "glpat-"),
	},
	{
		name: "aws",
		re:   regexp.MustCompile(`\b(?:AKIA|ASIA|AIDA|AROA)[0-9A-Z]{16}\b`),
	},
	{
		name: "aws-secret",
		re:   regexp.MustCompile(`(?i)\b(aws_secret_access_key|aws_secret_key)(\s*[:=]\s*["']?)([A-Za-z0-9/+=]{40})(["']?)`),
		replace: func(match string) string {
			sub := awsSecretSubmatch.FindStringSubmatch(match)
			if len(sub) != 5 || isObviousPlaceholder(sub[3]) {
				return match
			}
			return sub[1] + sub[2] + "[REDACTED:aws-secret]" + sub[4]
		},
	},
	{
		name: "google-api",
		re:   regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	},
	{
		name: "google-oauth",
		re:   regexp.MustCompile(`\bya29\.[0-9A-Za-z_-]{20,}\b`),
	},
	{
		name:    "huggingface",
		re:      regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}\b`),
		replace: redactUnlessPlaceholder("huggingface", "hf_"),
	},
	{
		name:    "npm",
		re:      regexp.MustCompile(`\bnpm_[A-Za-z0-9]{30,}\b`),
		replace: redactUnlessPlaceholder("npm", "npm_"),
	},
	{
		name: "slack",
		re:   regexp.MustCompile(`\bxox(?:e\.xox[bp]-|[baprs]-)[A-Za-z0-9-]{10,}\b|https://hooks\.slack\.com/services/T[A-Z0-9]{8,}/B[A-Z0-9]{8,}/[A-Za-z0-9]{20,}`),
	},
	{
		name:    "stripe",
		re:      regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[0-9A-Za-z]{20,}\b`),
		replace: redactUnlessPlaceholder("stripe", ""),
	},
	{
		name: "jwt",
		re:   regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
	},
	{
		name: "url-auth",
		re:   regexp.MustCompile(`\b((?:https?|postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|rediss|amqp|amqps)://[^\s/:@"'<>]+:)([^\s/@"'<>]{8,})(@[^\s/"'<>]+)`),
		replace: func(match string) string {
			sub := urlAuthSubmatch.FindStringSubmatch(match)
			if len(sub) != 4 || isURLPasswordPlaceholder(sub[2]) {
				return match
			}
			return sub[1] + "[REDACTED:url-auth]" + sub[3]
		},
	},
	{
		name: "bearer",
		re:   regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{24,}`),
		replace: func(match string) string {
			fields := strings.Fields(match)
			if len(fields) != 2 {
				return match
			}
			token := fields[1]
			// Avoid false positives on Unix file paths after prose word "bearer"
			// (e.g. "bearer /usr/local/share/documentation") or obvious placeholders.
			if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") || isObviousPlaceholder(token) {
				return match
			}
			return "[REDACTED:bearer]"
		},
	},
}

var (
	awsSecretSubmatch = regexp.MustCompile(`(?i)\b(aws_secret_access_key|aws_secret_key)(\s*[:=]\s*["']?)([A-Za-z0-9/+=]{40})(["']?)`)
	urlAuthSubmatch   = regexp.MustCompile(`\b((?:https?|postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|rediss|amqp|amqps)://[^\s/:@"'<>]+:)([^\s/@"'<>]{8,})(@[^\s/"'<>]+)`)
)

func redactUnlessPlaceholder(name, trimPrefix string) func(string) string {
	marker := "[REDACTED:" + name + "]"
	return func(match string) string {
		sample := match
		if trimPrefix != "" && strings.HasPrefix(sample, trimPrefix) {
			sample = strings.TrimPrefix(sample, trimPrefix)
		}
		if isObviousPlaceholder(sample) {
			return match
		}
		return marker
	}
}

func isObviousPlaceholder(token string) bool {
	if token == "" {
		return true
	}
	upper := strings.ToUpper(token)
	for _, hint := range []string{"YOUR_", "REPLACE_ME", "PLACEHOLDER", "REDACTED"} {
		if strings.Contains(upper, hint) {
			return true
		}
	}
	// Pure single-character masks such as "xxxxxxxxxxxxxxxxxxxxxxxx".
	trimmed := strings.Trim(token, "-_.")
	if len(trimmed) >= 8 {
		allSame := true
		first := trimmed[0]
		for i := 1; i < len(trimmed); i++ {
			if trimmed[i] != first {
				allSame = false
				break
			}
		}
		if allSame {
			return true
		}
	}
	return false
}

func isURLPasswordPlaceholder(pw string) bool {
	if isObviousPlaceholder(pw) {
		return true
	}
	lower := strings.ToLower(pw)
	switch lower {
	case "password", "changeme", "secret", "example", "your-password", "your_password":
		return true
	}
	if strings.HasPrefix(pw, "${") || strings.HasPrefix(pw, "$") || strings.Contains(pw, "*") {
		return true
	}
	return false
}

// String replaces recognized secrets with a stable marker.
func String(value string) string {
	for _, r := range rules {
		if r.replace != nil {
			value = r.re.ReplaceAllStringFunc(value, r.replace)
			continue
		}
		value = r.re.ReplaceAllString(value, "[REDACTED:"+r.name+"]")
	}
	return value
}

// Rules returns the names of the active redaction rules.
func Rules() []string {
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.name)
	}
	return names
}
