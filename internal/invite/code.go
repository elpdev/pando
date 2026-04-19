package invite

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/elpdev/pando/internal/identity"
)

func EncodeCode(bundle identity.InviteBundle) (string, error) {
	bytes, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("encode invite code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func DecodeCode(code string) (*identity.InviteBundle, error) {
	bytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code))
	if err != nil {
		return nil, fmt.Errorf("decode invite code: %w", err)
	}
	var bundle identity.InviteBundle
	if err := json.Unmarshal(bytes, &bundle); err != nil {
		return nil, fmt.Errorf("decode invite bundle: %w", err)
	}
	return &bundle, nil
}

func DecodeText(text string) (*identity.InviteBundle, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("invite input is empty")
	}
	if bundle, err := DecodeCode(ExtractCode(trimmed)); err == nil {
		return bundle, nil
	}
	var bundle identity.InviteBundle
	if err := json.Unmarshal([]byte(trimmed), &bundle); err == nil {
		return &bundle, nil
	}
	decoded, err := DecodeCode(ExtractCode(trimmed))
	if err == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("decode invite input: %w; try the value after 'invite-code:' or use pando identity invite-code --raw", err)
}

var inviteCodePattern = regexp.MustCompile(`(?m)invite-code:\s*([A-Za-z0-9_-]+)`)

func ExtractCode(text string) string {
	if matches := inviteCodePattern.FindStringSubmatch(text); len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, " ") || strings.Contains(line, ":") {
			continue
		}
		if _, err := base64.RawURLEncoding.DecodeString(line); err == nil {
			return line
		}
	}
	return strings.TrimSpace(text)
}
