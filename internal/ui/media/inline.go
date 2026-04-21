package media

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	_ "golang.org/x/image/webp"
)

type Protocol int

const (
	ProtocolNone Protocol = iota
	ProtocolKitty
	ProtocolITerm2
	ProtocolSixel
)

func DetectProtocol() Protocol {
	termProgram := os.Getenv("TERM_PROGRAM")
	term := os.Getenv("TERM")
	if termProgram == "iTerm.app" || termProgram == "WezTerm" {
		return ProtocolITerm2
	}
	if termProgram == "kitty" || strings.Contains(term, "kitty") {
		return ProtocolKitty
	}
	if supportsSixel(term) {
		return ProtocolSixel
	}
	return ProtocolNone
}

func RenderFile(path string, maxCols int) (string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read image: %w", err)
	}
	return RenderBytes(data, maxCols)
}

func RenderBytes(data []byte, maxCols int) (string, int, error) {
	protocol := DetectProtocol()
	if protocol == ProtocolNone || protocol == ProtocolSixel {
		return "", 0, nil
	}
	if maxCols < 8 {
		maxCols = 8
	}
	rows := estimateImageRows(data, maxCols)
	seq, err := renderBytes(data, maxCols, rows, protocol)
	if err != nil {
		return "", 0, err
	}
	if seq == "" {
		return "", 0, nil
	}
	seq = wrapPassthrough(seq)
	lines := make([]string, rows)
	lines[0] = seq
	return strings.Join(lines, "\n"), rows, nil
}

func ViewportPrefix() string {
	if DetectProtocol() != ProtocolKitty {
		return ""
	}
	return wrapPassthrough(ansi.KittyGraphics(nil, "a=d"))
}

func renderFile(path string, maxCols, rows int, protocol Protocol) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	return renderBytes(data, maxCols, rows, protocol)
}

func renderBytes(data []byte, maxCols, rows int, protocol Protocol) (string, error) {
	switch protocol {
	case ProtocolITerm2:
		encoded := base64.StdEncoding.EncodeToString(data)
		return ansi.ITerm2(fmt.Sprintf("File=inline=1;width=%d;preserveAspectRatio=1:%s", maxCols, encoded)), nil
	case ProtocolKitty:
		return renderKitty(data, maxCols, rows)
	default:
		return "", nil
	}
}

func renderKitty(data []byte, maxCols, rows int) (string, error) {
	payload, err := kittyPayload(data)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	const chunkSize = 4096
	var b strings.Builder
	for i := 0; i < len(encoded); i += chunkSize {
		end := i + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunk := encoded[i:end]
		first := i == 0
		last := end == len(encoded)
		opts := make([]string, 0, 9)
		if first {
			opts = append(opts, "a=T", "f=100", fmt.Sprintf("c=%d", maxCols), fmt.Sprintf("r=%d", rows), "C=1", "q=2", "z=-1")
		}
		switch {
		case first && !last:
			opts = append(opts, "m=1")
		case !first && !last:
			opts = append(opts, "m=1")
		case !first && last:
			opts = append(opts, "m=0")
		}
		b.WriteString(ansi.KittyGraphics([]byte(chunk), opts...))
	}
	return b.String(), nil
}

func kittyPayload(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, fmt.Errorf("encode kitty payload as png: %w", err)
	}
	return b.Bytes(), nil
}

func wrapPassthrough(seq string) string {
	if os.Getenv("TMUX") != "" {
		return ansi.TmuxPassthrough(seq)
	}
	term := os.Getenv("TERM")
	if os.Getenv("STY") != "" || strings.HasPrefix(term, "screen") {
		return ansi.ScreenPassthrough(seq, 760)
	}
	return seq
}

func supportsSixel(term string) bool {
	for _, marker := range []string{"sixel", "mlterm", "yaft"} {
		if strings.Contains(term, marker) {
			return true
		}
	}
	return false
}

func estimateImageRows(data []byte, maxCols int) int {
	const (
		minRows = 4
		maxRows = 18
	)
	if len(data) == 0 {
		return min(maxCols/2, maxRows)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return min(max(maxCols/2, minRows), maxRows)
	}
	rows := int(math.Round((float64(config.Height) / float64(config.Width)) * float64(maxCols) * 0.5))
	if rows < minRows {
		return minRows
	}
	if rows > maxRows {
		return maxRows
	}
	return rows
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
