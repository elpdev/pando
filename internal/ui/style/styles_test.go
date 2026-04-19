package style

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFormatFingerprint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abcd", "abcd"},
		{"abcdef", "abcd·ef"},
		{"abcdef0123456789", "abcd·ef01·2345·6789"},
		{"abcdef012345", "abcd·ef01·2345"},
	}
	for _, c := range cases {
		if got := FormatFingerprint(c.in); got != c.want {
			t.Errorf("FormatFingerprint(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatFingerprintShort(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"abcdef", "abcd·ef"},
		{"abcdef0123456789", "abcd·ef01"},
	}
	for _, c := range cases {
		if got := FormatFingerprintShort(c.in); got != c.want {
			t.Errorf("FormatFingerprintShort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPeerAccentIsStable(t *testing.T) {
	fp := "abcdef0123456789"
	first := PeerAccent(fp)
	for i := 0; i < 20; i++ {
		if got := PeerAccent(fp); got != first {
			t.Fatalf("PeerAccent changed across calls: run %d got %v, want %v", i, got, first)
		}
	}
}

func TestPeerAccentEmptyFingerprintFallsBackToOk(t *testing.T) {
	if PeerAccent("") != colorOk {
		t.Errorf("empty fingerprint should map to ok color, got %v", PeerAccent(""))
	}
}

func TestPeerAccentCoversPalette(t *testing.T) {
	// A handful of synthetic fingerprints should cover >=4 distinct palette
	// entries. This is a coarse check that the hash is actually distributing.
	seen := map[lipgloss.Color]struct{}{}
	fps := []string{
		"aaaa0000", "bbbb1111", "cccc2222", "dddd3333",
		"eeee4444", "ffff5555", "1010abcd", "2020beef",
		"3030cafe", "4040face", "5050dead", "6060f00d",
	}
	for _, fp := range fps {
		seen[PeerAccent(fp)] = struct{}{}
	}
	if len(seen) < 4 {
		t.Errorf("peer accent hash too clumpy: only %d distinct colors across %d fingerprints", len(seen), len(fps))
	}
}

func TestPeerAccentStyleMatchesPeerAccent(t *testing.T) {
	fp := "abcdef0123456789"
	want := PeerAccent(fp)
	got := PeerAccentStyle(fp).GetForeground()
	if got != lipgloss.TerminalColor(want) {
		t.Errorf("PeerAccentStyle foreground %v != PeerAccent %v", got, want)
	}
}

func TestTokensCarryColor(t *testing.T) {
	// Guard against accidental zero-value foreground/background tokens.
	// Lipgloss may suppress ANSI output when stdout isn't a terminal (as in
	// `go test`), so we assert on the style's configured color instead of the
	// rendered string.
	fgCases := []struct {
		name string
		s    lipgloss.Style
	}{
		{"Muted", Muted},
		{"Subtle", Subtle},
		{"Dim", Dim},
		{"Bright", Bright},
		{"StatusOk", StatusOk},
		{"StatusWarn", StatusWarn},
		{"StatusBad", StatusBad},
		{"StatusInfo", StatusInfo},
	}
	noColor := lipgloss.NoColor{}
	for _, c := range fgCases {
		if c.s.GetForeground() == noColor {
			t.Errorf("token %s has no foreground color set", c.name)
		}
	}
	if Selected.GetBackground() == noColor {
		t.Error("Selected has no background color set")
	}
	if Modal.GetBackground() == noColor {
		t.Error("Modal has no background color set")
	}
}

func TestGlyphsAreNonEmpty(t *testing.T) {
	glyphs := []string{
		GlyphConnected, GlyphReconnecting, GlyphOffline, GlyphAuthFailed,
		GlyphVerified, GlyphUnverified,
		GlyphDeliveryPending, GlyphDeliverySent, GlyphDeliveryDelivered, GlyphDeliveryFailed,
		GlyphCursorRow, GlyphActiveChat, GlyphUnreadDot, GlyphJumpToLatest,
		GroupSep,
	}
	for i, g := range glyphs {
		if g == "" {
			t.Errorf("glyph at index %d is empty", i)
		}
	}
}
