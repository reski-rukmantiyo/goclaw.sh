package skills

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GuardViolation describes a security pattern match in SKILL.md content.
type GuardViolation struct {
	Pattern string
	Reason  string
	Line    int
}

// guardRule pairs a compiled regexp with a human-readable rejection reason.
type guardRule struct {
	re     *regexp.Regexp
	reason string
}

// skillGuardRules contains high-signal security patterns for SKILL.md content.
// Focus: shell injection, destructive ops, obfuscated payloads, credential exfil,
// path traversal, SQL injection, privilege escalation.
//
// Not exhaustive — GoClaw's exec tool has its own runtime deny-list.
// This scanner runs BEFORE the file is written, blocking poisoned skills at creation time.
var skillGuardRules = []guardRule{
	// --- Destructive shell operations ---
	{regexp.MustCompile(`(?i)rm\s+-rf\s+[/~]`),                     "destructive rm -rf on root/home"},
	{regexp.MustCompile(`:\s*\|\s*:\s*&`),                            "fork bomb (self-pipe-to-background pattern)"},
	{regexp.MustCompile(`(?i)\bdd\b.+\bof=/dev/`),                  "disk overwrite via dd"},
	{regexp.MustCompile(`(?i)\bmkfs\b`),                             "filesystem format command"},
	{regexp.MustCompile(`(?i)\bshred\s`),                            "file shredding command"},

	// --- Code injection / obfuscation ---
	{regexp.MustCompile(`(?i)base64\s+(--decode|-d)\s*\|`),         "base64 decode piped to shell"},
	{regexp.MustCompile(`(?i)eval\s*\(\s*base64`),                   "eval of base64-encoded payload"},
	{regexp.MustCompile(`(?i)eval\s*\$\s*\(`),                       "eval of subshell output"},
	{regexp.MustCompile(`(?i)curl\s+\S+\s*\|\s*(ba)?sh`),           "curl pipe to shell"},
	{regexp.MustCompile(`(?i)wget\s+\S+\s*\|\s*(ba)?sh`),           "wget pipe to shell"},
	{regexp.MustCompile(`(?i)python[23]?\s+-c\s+.{0,40}exec\s*\(`), "python exec injection"},
	{regexp.MustCompile(`(?i)__import__\s*\(\s*['"]os['"]`),        "Python os import obfuscation"},

	// --- Credential / data exfiltration ---
	{regexp.MustCompile(`(?i)/etc/passwd\b`),                        "system password file access"},
	{regexp.MustCompile(`(?i)/etc/shadow\b`),                        "shadow password file access"},
	{regexp.MustCompile(`(?i)\.ssh/id_rsa`),                         "private SSH key access"},
	{regexp.MustCompile(`(?i)AWS_SECRET_ACCESS_KEY`),                "AWS credential reference"},
	{regexp.MustCompile(`(?i)GOCLAW_DB_URL\b`),                     "GoClaw database credential"},
	{regexp.MustCompile(`(?i)(curl|wget)\s+\S+.*\$\{?(HOME|USER|PASS|KEY|SECRET|TOKEN)`), "env var exfiltration via HTTP"},

	// --- Path traversal ---
	{regexp.MustCompile(`\.\./\.\./\.\./`),                          "deep path traversal (../../..)"},

	// --- SQL injection ---
	{regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`),                     "SQL DROP TABLE"},
	{regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`),                 "SQL TRUNCATE TABLE"},
	{regexp.MustCompile(`(?i)\bDROP\s+DATABASE\b`),                  "SQL DROP DATABASE"},

	// --- Privilege escalation ---
	{regexp.MustCompile(`(?i)\bsudo\s`),                             "sudo usage"},
	{regexp.MustCompile(`(?i)\bchmod\s+[0-7]*[2367]\b`),             "world-writable chmod (others write bit set)"},
	{regexp.MustCompile(`(?i)\bchown\s+root\b`),                     "chown to root"},
}

// GuardSkillContent scans SKILL.md content for security violations before any disk write.
// Returns (violations, safe). safe=true means no violations found and the content may be written.
//
// Scanning is line-by-line: one violation per line (first matching rule wins).
// Hard-reject on ANY violation — no partial allow.
func GuardSkillContent(content string) ([]GuardViolation, bool) {
	lines := strings.Split(content, "\n")
	var violations []GuardViolation

	for lineNum, line := range lines {
		for _, rule := range skillGuardRules {
			if rule.re.MatchString(line) {
				violations = append(violations, GuardViolation{
					Pattern: rule.re.String(),
					Reason:  rule.reason,
					Line:    lineNum + 1,
				})
				break // one violation per line is sufficient
			}
		}
	}

	return violations, len(violations) == 0
}

// GuardSkillScope checks whether skill content is within the agent's defined scope.
// Returns (reason, allowed). allowed=true means the content passes scope checks.
// When allowed=false, reason describes which scope boundary was violated.
func GuardSkillScope(content string, cfg *store.ScopeGuardrailsConfig) (string, bool) {
	if cfg == nil || !cfg.Enabled {
		return "", true
	}
	lower := strings.ToLower(content)

	// Denied topics — hard block
	for _, topic := range cfg.DeniedTopics {
		if topic == "" {
			continue
		}
		lt := strings.ToLower(topic)
		if strings.Contains(lower, lt) {
			return fmt.Sprintf("skill content matches denied topic %q — outside agent scope", topic), false
		}
	}

	// Allowed topics — if defined, content must relate to at least one
	if len(cfg.AllowedTopics) > 0 {
		matched := false
		for _, topic := range cfg.AllowedTopics {
			if topic == "" {
				continue
			}
			if strings.Contains(lower, strings.ToLower(topic)) {
				matched = true
				break
			}
		}
		if !matched {
			return "skill content does not match any allowed topic — outside agent scope", false
		}
	}

	return "", true
}

// FormatGuardViolations returns a human-readable rejection message suitable
// for returning to the LLM as a tool error result.
func FormatGuardViolations(violations []GuardViolation) string {
	var sb strings.Builder
	sb.WriteString("Skill content rejected by security scanner. Violations:\n")
	for _, v := range violations {
		sb.WriteString(fmt.Sprintf("  - Line %d: %s\n", v.Line, v.Reason))
	}
	sb.WriteString("\nRemove the flagged content and try again.")
	return sb.String()
}
