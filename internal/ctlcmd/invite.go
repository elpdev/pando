package ctlcmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/elpdev/pando/internal/identity"
	"github.com/makiuchi-d/gozxing"
	gozxingqr "github.com/makiuchi-d/gozxing/qrcode"
	_ "image/jpeg"
	_ "image/png"
)

type inviteInputOptions struct {
	InvitePath    string
	InviteCode    string
	ReadStdin     bool
	ReadPaste     bool
	ReadClipboard bool
	QRImagePath   string
}

func validateInviteInputFlags(invitePath, inviteCode string, readStdin, readPaste, fromClipboard bool, qrImagePath string) error {
	inputs := 0
	if strings.TrimSpace(invitePath) != "" {
		inputs++
	}
	if strings.TrimSpace(inviteCode) != "" {
		inputs++
	}
	if readStdin {
		inputs++
	}
	if readPaste {
		inputs++
	}
	if fromClipboard {
		inputs++
	}
	if strings.TrimSpace(qrImagePath) != "" {
		inputs++
	}
	if inputs == 0 {
		return fmt.Errorf("provide one of -invite, -code, -stdin, -paste, -from-clipboard, or -qr-image")
	}
	if inputs > 1 {
		return fmt.Errorf("use only one of -invite, -code, -stdin, -paste, -from-clipboard, or -qr-image")
	}
	return nil
}

func encodeInviteCode(bundle identity.InviteBundle) (string, error) {
	bytes, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("encode invite code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func decodeInviteCode(code string) (*identity.InviteBundle, error) {
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

func readInviteBundle(input inviteInputOptions) (*identity.InviteBundle, error) {
	switch {
	case strings.TrimSpace(input.InviteCode) != "":
		return decodeInviteText(input.InviteCode)
	case input.ReadClipboard:
		text, err := clipboard.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("read invite from clipboard: %w", err)
		}
		return decodeInviteText(text)
	case strings.TrimSpace(input.QRImagePath) != "":
		return readInviteBundleFromQRImage(input.QRImagePath)
	case input.ReadStdin || input.ReadPaste || input.InvitePath == "-":
		if input.ReadPaste {
			fmt.Fprintln(os.Stderr, "paste the invite, then press Ctrl-D when finished:")
		}
		bytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read invite from stdin: %w", err)
		}
		return decodeInviteText(string(bytes))
	case strings.TrimSpace(input.InvitePath) != "":
		bytes, err := os.ReadFile(input.InvitePath)
		if err != nil {
			return nil, err
		}
		return decodeInviteText(string(bytes))
	default:
		return nil, fmt.Errorf("provide one of -invite, -code, -stdin, -paste, -from-clipboard, or -qr-image")
	}
}

func readInviteBundleFromQRImage(path string) (*identity.InviteBundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open QR image: %w", err)
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode QR image: %w", err)
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("read QR image: %w", err)
	}
	result, err := gozxingqr.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		return nil, fmt.Errorf("read QR image: %w", err)
	}
	return decodeInviteText(result.GetText())
}

func decodeInviteText(text string) (*identity.InviteBundle, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("invite input is empty")
	}
	if bundle, err := decodeInviteCode(extractInviteCode(trimmed)); err == nil {
		return bundle, nil
	}
	var bundle identity.InviteBundle
	if err := json.Unmarshal([]byte(trimmed), &bundle); err == nil {
		return &bundle, nil
	}
	code := extractInviteCode(trimmed)
	decoded, err := decodeInviteCode(code)
	if err == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("decode invite input: %w; try the value after 'invite-code:' or use pando identity invite-code --raw", err)
}

var inviteCodePattern = regexp.MustCompile(`(?m)invite-code:\s*([A-Za-z0-9_-]+)`)

func extractInviteCode(text string) string {
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
