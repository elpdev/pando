package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/ui/style"
)

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

	titleWidth := contentWidth - metaWidth
	if meta != "" {
		titleWidth--
	}
	if titleWidth < 1 {
		titleWidth = 1
	}
	header := lipgloss.NewStyle().Width(titleWidth).MaxWidth(titleWidth).Render(titleText)
	if meta != "" {
		header += " " + lipgloss.NewStyle().Width(metaWidth).Align(lipgloss.Right).Render(metaStyle.Render(meta))
	}
	lines := []string{header}
	if detail != "" {
		lines = append(lines, detailStyle.Width(contentWidth).MaxWidth(contentWidth).Render(detail))
	}
	return rowStyle.Render(strings.Join(lines, "\n"))
}

func renderPaletteHeader(title, subtitle string, width int) string {
	if width < 1 {
		width = 1
	}
	accent := style.PaletteAccent.Render("●")
	header := fmt.Sprintf("%s %s", accent, style.PaletteTitle.Render(title))
	parts := []string{header}
	if subtitle != "" {
		parts = append(parts, style.PaletteMeta.Width(width).Render(subtitle))
	}
	parts = append(parts, style.Faint.Render(strings.Repeat("─", max(8, min(width, lipgloss.Width(title)+6)))))
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
