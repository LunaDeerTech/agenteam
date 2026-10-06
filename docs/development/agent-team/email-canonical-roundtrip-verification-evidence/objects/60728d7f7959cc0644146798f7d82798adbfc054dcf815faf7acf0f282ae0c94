package account

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

func NormalizeEmail(input string) (string, error) {
	if len(input) == 0 || len(input) > 254 || strings.TrimSpace(input) != input {
		return "", field("/email", "EMAIL_INVALID")
	}
	for i := range len(input) {
		if input[i] < 33 || input[i] > 126 {
			return "", field("/email", "EMAIL_INVALID")
		}
	}
	canonical := strings.ToLower(input)
	if bareEmailAddress(input) {
		return canonical, nil
	}
	// Preserve the old accepted set, adding only its complete lowercase
	// outputs. Go's parser requires this one domain-literal marker's case.
	local, domain, ok := strings.Cut(input, "@")
	if input == canonical && ok && !strings.Contains(domain, "@") && strings.HasPrefix(domain, "[ipv6:") && strings.HasSuffix(domain, "]") {
		wire := local + "@[IPv6:" + domain[len("[ipv6:"):]
		if bareEmailAddress(wire) {
			return canonical, nil
		}
	}
	return "", field("/email", "EMAIL_INVALID")
}

func bareEmailAddress(input string) bool {
	a, err := mail.ParseAddress(input)
	return err == nil && a.Name == "" && a.Address == input && !strings.ContainsAny(input, "()<>,;\\\"")
}

var reservedNames = map[string]bool{"api": true, "assets": true, "auth": true, "login": true, "logout": true, "invite": true, "reset": true, "settings": true, "system": true, "personal": true, "diagnostics": true, "livez": true, "readyz": true, "debug": true, "support": true, "root": true, "admin": true}

func NormalizeUsername(input string) (string, error) {
	if len(input) < 3 || len(input) > 32 {
		return "", field("/username", "USERNAME_INVALID")
	}
	s := strings.ToLower(input)
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0 && i < len(s)-1) {
			return "", field("/username", "USERNAME_INVALID")
		}
	}
	if reservedNames[s] {
		return "", field("/username", "USERNAME_RESERVED")
	}
	return s, nil
}
func ValidateDisplayName(s string) error {
	if !utf8.ValidString(s) || len(s) > 320 || utf8.RuneCountInString(s) > 80 {
		return field("/display_name", "DISPLAY_NAME_INVALID")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return field("/display_name", "DISPLAY_NAME_INVALID")
		}
	}
	return nil
}
