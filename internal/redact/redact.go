// Package redact removes high-confidence secrets from memory text before it
// is written. It is deliberately conservative: patterns must match well-known
// credential shapes, so ordinary prose and example placeholders survive.
package redact

import "regexp"

type rule struct {
	name string
	re   *regexp.Regexp
}

var rules = []rule{
	{"private-key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)},
	{"openai", regexp.MustCompile(`\bsk-(?:proj-|ant-)?[A-Za-z0-9_-]{20,}\b`)},
	{"github", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)},
	{"github-pat", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`)},
	{"aws", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"google-api", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"slack", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"stripe", regexp.MustCompile(`\bsk_live_[0-9A-Za-z]{20,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{24,}`)},
}

// String replaces recognized secrets with a stable marker.
func String(value string) string {
	for _, r := range rules {
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
