package chat

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/elpdev/pando/internal/ui/style"
)

const ansiReset = "\x1b[0m"

func paletteWidth(totalWidth int) int {
	return min(max(56, totalWidth*3/5), max(44, totalWidth-8))
}

func paletteHeight(totalHeight int) int {
	return min(max(14, totalHeight*2/3), max(10, totalHeight-4))
}

func renderPaletteOverlay(width, height int, title, subtitle string, bodyParts []string, footer string) string {
	modalWidth := paletteWidth(width)
	modalHeight := paletteHeight(height)
	if modalWidth <= 0 || modalHeight <= 0 {
		return ""
	}

	parts := []string{renderPaletteHeader(title, subtitle, modalWidth-4)}
	parts = append(parts, bodyParts...)
	if footer != "" {
		parts = append(parts, style.PaletteFooter.Render(footer))
	}

	body := strings.Join(parts, "\n\n")
	modal := style.PaletteModal.Width(modalWidth).Padding(1, 2).Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceBackground(style.BackdropTint))
}

// renderFloatingPaletteOverlay composes the palette modal on top of the
// already-rendered base view. Unlike renderPaletteOverlay, it does not tint or
// replace the surrounding cells, so the chat remains visible around the modal.
func renderFloatingPaletteOverlay(base string, width, height int, title, subtitle string, bodyParts []string, footer string) string {
	modalWidth := paletteWidth(width)
	modalHeight := paletteHeight(height)
	if modalWidth <= 0 || modalHeight <= 0 {
		return base
	}

	parts := []string{renderPaletteHeader(title, subtitle, modalWidth-4)}
	parts = append(parts, bodyParts...)
	if footer != "" {
		parts = append(parts, style.PaletteFooter.Render(footer))
	}
	body := strings.Join(parts, "\n\n")
	modal := style.PaletteModal.Width(modalWidth).Padding(1, 2).Render(body)
	return overlayCenter(base, modal, width, height)
}

// overlayCenter layers modal on top of base at the centered position of a
// (width, height) terminal. Both inputs may contain ANSI escape codes; cuts
// are made on cell boundaries via charmbracelet/x/ansi.
func overlayCenter(base, modal string, width, height int) string {
	if width <= 0 || height <= 0 {
		return base
	}
	baseLines := strings.Split(base, "\n")
	// Pad base to height rows so we can place the modal even if the base is
	// short.
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}
	modalLines := strings.Split(modal, "\n")
	modalHeight := len(modalLines)
	modalWidth := 0
	for _, line := range modalLines {
		if w := ansi.StringWidth(line); w > modalWidth {
			modalWidth = w
		}
	}
	top := max(0, (height-modalHeight)/2)
	left := max(0, (width-modalWidth)/2)

	for i, ml := range modalLines {
		y := top + i
		if y >= len(baseLines) {
			break
		}
		baseLine := baseLines[y]
		baseWidth := ansi.StringWidth(baseLine)
		// Pad base line with spaces so the left cut reaches the modal's left
		// edge even when the underlying line is shorter.
		if baseWidth < left {
			baseLine = baseLine + strings.Repeat(" ", left-baseWidth)
			baseWidth = left
		}
		leftPart := ansi.Cut(baseLine, 0, left)
		cutRight := left + ansi.StringWidth(ml)
		rightPart := ""
		if baseWidth > cutRight {
			rightPart = ansi.Cut(baseLine, cutRight, baseWidth)
		}
		baseLines[y] = leftPart + ansiReset + ml + ansiReset + rightPart
	}
	// Trim any trailing padding rows that the caller did not render, preserving
	// the base view's original trailing newline behavior.
	return strings.Join(baseLines, "\n")
}

func renderPaletteListItem(width int, selected bool, title, detail, meta string) string {
	return renderPaletteListItemMatched(width, selected, title, detail, meta, nil)
}

func renderPaletteListItemMatched(width int, selected bool, title, detail, meta string, matched map[int]struct{}) string {
	contentWidth := max(1, width-2)
	metaWidth := lipgloss.Width(meta)
	detailStyle := style.PaletteMeta
	metaStyle := style.PaletteShortcut
	rowStyle := style.PaletteItem.Width(width)
	titleText := title
	if selected {
		rowStyle = style.PaletteSelectedItem.Width(width)
		detailStyle = style.Dim
		metaStyle = style.Bright
		titleText = style.Bright.Bold(true).Render(title)
	} else {
		titleText = style.Bright.Render(title)
	}
	if len(matched) > 0 {
		titleText = renderPaletteMatches(title, matched, selected)
	}

	marker := "  "
	if selected {
		marker = style.PaletteAccent.Render(style.GlyphPrompt) + " "
	}
	const markerWidth = 2
	innerWidth := max(1, contentWidth-markerWidth)
	titleWidth := innerWidth - metaWidth
	if meta != "" {
		titleWidth--
	}
	if titleWidth < 1 {
		titleWidth = 1
	}
	pad := lipgloss.NewStyle().Background(style.BackdropTint)
	header := marker + pad.Width(titleWidth).MaxWidth(titleWidth).Render(titleText)
	if meta != "" {
		header += " " + pad.Width(metaWidth).Align(lipgloss.Right).Render(metaStyle.Render(meta))
	}
	lines := []string{header}
	if detail != "" {
		lines = append(lines, "  "+detailStyle.Background(style.BackdropTint).Width(innerWidth).MaxWidth(innerWidth).Render(detail))
	}
	return rowStyle.Render(strings.Join(lines, "\n"))
}

func renderPaletteHeader(title, subtitle string, width int) string {
	if width < 1 {
		width = 1
	}
	rendered := style.PaletteTitle.Render(title)
	slashCount := max(3, width-lipgloss.Width(rendered)-1)
	slashes := style.PaletteAccent.Render(strings.Repeat("/", slashCount))
	parts := []string{rendered + " " + slashes}
	if subtitle != "" {
		parts = append(parts, style.PaletteMeta.Width(width).Render(subtitle))
	}
	return strings.Join(parts, "\n")
}

func renderPaletteMatches(s string, matched map[int]struct{}, selected bool) string {
	var b strings.Builder
	for idx, r := range []rune(s) {
		segment := string(r)
		if _, ok := matched[idx]; ok {
			if selected {
				segment = style.PaletteMatch.Bold(true).Render(segment)
			} else {
				segment = style.PaletteMatch.Render(segment)
			}
		} else if selected {
			segment = style.Bright.Bold(true).Render(segment)
		} else {
			segment = style.Bright.Render(segment)
		}
		b.WriteString(segment)
	}
	return b.String()
}

func subsequenceMatch(s, query string) map[int]struct{} {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}
	runes := []rune(s)
	queryRunes := []rune(query)
	matched := make(map[int]struct{}, len(queryRunes))
	q := 0
	for i, r := range runes {
		if q >= len(queryRunes) {
			break
		}
		if []rune(strings.ToLower(string(r)))[0] == queryRunes[q] {
			matched[i] = struct{}{}
			q++
		}
	}
	if q != len(queryRunes) {
		return nil
	}
	return matched
}
