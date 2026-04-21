package style

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestThemesArePopulated(t *testing.T) {
	// Every theme in the registry must fill every named color. Zero-value
	// lipgloss.Color is an empty string that renders as NoColor — a silent
	// footgun. Catch it at test time rather than at runtime.
	required := []struct {
		name string
		get  func(Theme) lipgloss.Color
	}{
		{"Faint", func(th Theme) lipgloss.Color { return th.Faint }},
		{"Muted", func(th Theme) lipgloss.Color { return th.Muted }},
		{"Subtle", func(th Theme) lipgloss.Color { return th.Subtle }},
		{"Dim", func(th Theme) lipgloss.Color { return th.Dim }},
		{"Bright", func(th Theme) lipgloss.Color { return th.Bright }},
		{"BgSel", func(th Theme) lipgloss.Color { return th.BgSel }},
		{"BgModal", func(th Theme) lipgloss.Color { return th.BgModal }},
		{"BgPalette", func(th Theme) lipgloss.Color { return th.BgPalette }},
		{"Divider", func(th Theme) lipgloss.Color { return th.Divider }},
		{"Ok", func(th Theme) lipgloss.Color { return th.Ok }},
		{"Warn", func(th Theme) lipgloss.Color { return th.Warn }},
		{"Bad", func(th Theme) lipgloss.Color { return th.Bad }},
		{"Info", func(th Theme) lipgloss.Color { return th.Info }},
		{"BannerText", func(th Theme) lipgloss.Color { return th.BannerText }},
		{"BannerSlash", func(th Theme) lipgloss.Color { return th.BannerSlash }},
		{"RoomAccent", func(th Theme) lipgloss.Color { return th.RoomAccent }},
	}
	for name, th := range Themes {
		if th.Name == "" {
			t.Errorf("theme %q has empty Name field", name)
		}
		for _, r := range required {
			if r.get(th) == "" {
				t.Errorf("theme %q: %s is unset", name, r.name)
			}
		}
		if len(th.PeerAccents) < 4 {
			t.Errorf("theme %q: PeerAccents has %d entries, need >=4 for hash distribution", name, len(th.PeerAccents))
		}
		for i, c := range th.PeerAccents {
			if c == "" {
				t.Errorf("theme %q: PeerAccents[%d] is unset", name, i)
			}
		}
	}
}

func TestApplySwapsActive(t *testing.T) {
	// Preserve whatever theme is active so the rest of the suite isn't
	// perturbed.
	prev := Current()
	t.Cleanup(func() { Apply(prev) })

	Apply(Themes["phosphor"])
	if Current().Name != "phosphor" {
		t.Fatalf("expected active theme to be phosphor, got %q", Current().Name)
	}
	// Spot-check one token picks up the new palette.
	if StatusInfo.GetForeground() != lipgloss.TerminalColor(lipgloss.Color("#6FD0E3")) {
		t.Errorf("StatusInfo did not pick up phosphor cyan, got %v", StatusInfo.GetForeground())
	}

	Apply(Themes["classic"])
	if Current().Name != "classic" {
		t.Fatalf("expected active theme to be classic after swap back, got %q", Current().Name)
	}
	if StatusInfo.GetForeground() != lipgloss.TerminalColor(lipgloss.Color("69")) {
		t.Errorf("StatusInfo did not restore classic info color, got %v", StatusInfo.GetForeground())
	}
}

func TestDefaultThemeIsPhosphor(t *testing.T) {
	// Guard that no accidental rename splits DefaultThemeName from the
	// registry; both must agree or init() applies a zero theme and every
	// color renders as NoColor.
	if _, ok := Themes[DefaultThemeName]; !ok {
		t.Fatalf("DefaultThemeName %q not present in Themes", DefaultThemeName)
	}
	if DefaultThemeName != "phosphor" {
		t.Errorf("expected DefaultThemeName to be phosphor, got %q", DefaultThemeName)
	}
}
