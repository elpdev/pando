package ctlcmd

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/store"
	"rsc.io/qr"
)

func TestEjectForce(t *testing.T) {
	dataDir := t.TempDir()
	mailbox := "alice"
	clientStore := store.NewClientStore(dataDir)
	if _, _, err := clientStore.LoadOrCreateIdentity(mailbox); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "identity.json")); err != nil {
		t.Fatalf("expected identity file to exist before eject: %v", err)
	}

	if err := runEject([]string{"-mailbox", mailbox, "-data-dir", dataDir, "-force"}); err != nil {
		t.Fatalf("eject: %v", err)
	}

	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("expected data dir to be removed after eject")
	}
}

func TestEjectConfirmation(t *testing.T) {
	dataDir := t.TempDir()
	mailbox := "bob"
	clientStore := store.NewClientStore(dataDir)
	if _, _, err := clientStore.LoadOrCreateIdentity(mailbox); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	// Patch stdin to simulate user typing the mailbox name.
	origStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = origStdin })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	if _, err := w.WriteString(mailbox + "\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	w.Close()

	if err := runEject([]string{"-mailbox", mailbox, "-data-dir", dataDir}); err != nil {
		t.Fatalf("eject: %v", err)
	}

	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("expected data dir to be removed after eject")
	}
}

func TestEjectConfirmationAbort(t *testing.T) {
	dataDir := t.TempDir()
	mailbox := "carol"
	clientStore := store.NewClientStore(dataDir)
	if _, _, err := clientStore.LoadOrCreateIdentity(mailbox); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	origStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = origStdin })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	if _, err := w.WriteString("wrong-mailbox\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	w.Close()

	err = runEject([]string{"-mailbox", mailbox, "-data-dir", dataDir})
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("expected aborted error, got %v", err)
	}

	// Data dir should still exist.
	if _, statErr := os.Stat(dataDir); statErr != nil {
		t.Fatalf("expected data dir to survive aborted eject: %v", statErr)
	}
}

func TestInviteCodeRoundTrip(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := encodeInviteCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	bundle, err := decodeInviteCode(code)
	if err != nil {
		t.Fatalf("decode invite code: %v", err)
	}
	if bundle.AccountID != id.AccountID {
		t.Fatalf("expected account id %s, got %s", id.AccountID, bundle.AccountID)
	}
	if len(bundle.Devices) != len(id.InviteBundle().Devices) {
		t.Fatalf("expected %d devices, got %d", len(id.InviteBundle().Devices), len(bundle.Devices))
	}
}

func TestExtractInviteCodeFromVerboseOutput(t *testing.T) {
	text := "account: leo\nfingerprint: abcdef0123456789\ninvite-code: raw-invite-code\n"
	if got := extractInviteCode(text); got != "raw-invite-code" {
		t.Fatalf("expected invite code extraction, got %q", got)
	}
}

func TestDecodeInviteTextAcceptsVerboseInviteOutput(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := encodeInviteCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	bundle, err := decodeInviteText("account: alice\nfingerprint: " + id.Fingerprint() + "\ninvite-code: " + code + "\n")
	if err != nil {
		t.Fatalf("decode verbose invite text: %v", err)
	}
	if bundle.AccountID != "alice" {
		t.Fatalf("expected alice bundle, got %q", bundle.AccountID)
	}
}

func TestRunImportContactWithStdin(t *testing.T) {
	aliceDir := t.TempDir()
	bobDir := t.TempDir()
	aliceStore := store.NewClientStore(aliceDir)
	bobStore := store.NewClientStore(bobDir)
	aliceID, _, err := aliceStore.LoadOrCreateIdentity("alice")
	if err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	if _, _, err := bobStore.LoadOrCreateIdentity("bob"); err != nil {
		t.Fatalf("create bob identity: %v", err)
	}
	code, err := encodeInviteCode(aliceID.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}

	withPatchedStdin(t, code+"\n", func() {
		if err := runImportContactWithName("add-contact", []string{"-mailbox", "bob", "-data-dir", bobDir, "-stdin"}); err != nil {
			t.Fatalf("import contact from stdin: %v", err)
		}
	})

	contact, err := bobStore.LoadContact("alice")
	if err != nil {
		t.Fatalf("load alice contact: %v", err)
	}
	if contact.AccountID != "alice" {
		t.Fatalf("expected alice contact, got %q", contact.AccountID)
	}
}

func TestRunImportContactWithPaste(t *testing.T) {
	aliceDir := t.TempDir()
	bobDir := t.TempDir()
	aliceStore := store.NewClientStore(aliceDir)
	bobStore := store.NewClientStore(bobDir)
	aliceID, _, err := aliceStore.LoadOrCreateIdentity("alice")
	if err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	if _, _, err := bobStore.LoadOrCreateIdentity("bob"); err != nil {
		t.Fatalf("create bob identity: %v", err)
	}
	code, err := encodeInviteCode(aliceID.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	pasted := "account: alice\nfingerprint: " + aliceID.Fingerprint() + "\ninvite-code: " + code + "\n"

	withPatchedStdin(t, pasted, func() {
		if err := runImportContactWithName("add-contact", []string{"-mailbox", "bob", "-data-dir", bobDir, "-paste"}); err != nil {
			t.Fatalf("import contact from paste: %v", err)
		}
	})

	if _, err := bobStore.LoadContact("alice"); err != nil {
		t.Fatalf("load imported alice contact: %v", err)
	}
}

func TestRunInviteCodeRaw(t *testing.T) {
	dataDir := t.TempDir()
	clientStore := store.NewClientStore(dataDir)
	id, _, err := clientStore.LoadOrCreateIdentity("alice")
	if err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	code, err := encodeInviteCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInviteCode([]string{"-mailbox", "alice", "-data-dir", dataDir, "-raw"}); err != nil {
			t.Fatalf("run invite code raw: %v", err)
		}
	})
	if strings.TrimSpace(output) != code {
		t.Fatalf("expected raw invite output %q, got %q", code, strings.TrimSpace(output))
	}
}

func TestRunInviteCodeDefaultShowsNextStep(t *testing.T) {
	dataDir := t.TempDir()
	clientStore := store.NewClientStore(dataDir)
	if _, _, err := clientStore.LoadOrCreateIdentity("alice"); err != nil {
		t.Fatalf("create alice identity: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInviteCode([]string{"-mailbox", "alice", "-data-dir", dataDir}); err != nil {
			t.Fatalf("run invite code: %v", err)
		}
	})
	if !strings.Contains(output, "the other person can import it with: pandoctl add-contact --mailbox <their-mailbox> --paste") {
		t.Fatalf("expected next-step guidance in output, got %q", output)
	}
}

func TestReadInviteBundleFromQRImage(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := encodeInviteCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	qrCode, err := qr.Encode(code, qr.L)
	if err != nil {
		t.Fatalf("encode QR image: %v", err)
	}
	qrCode.Scale = 12
	qrImage := qrCode.Image()
	padded := image.NewRGBA(image.Rect(0, 0, qrImage.Bounds().Dx()+80, qrImage.Bounds().Dy()+80))
	draw.Draw(padded, padded.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(padded, image.Rect(40, 40, 40+qrImage.Bounds().Dx(), 40+qrImage.Bounds().Dy()), qrImage, image.Point{}, draw.Src)
	path := filepath.Join(t.TempDir(), "invite.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create qr file: %v", err)
	}
	if err := png.Encode(file, padded); err != nil {
		t.Fatalf("write qr image: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close qr file: %v", err)
	}

	bundle, err := readInviteBundle(inviteInputOptions{QRImagePath: path})
	if err != nil {
		t.Fatalf("read invite bundle from qr image: %v", err)
	}
	if bundle.AccountID != id.AccountID {
		t.Fatalf("expected account %q, got %q", id.AccountID, bundle.AccountID)
	}
}

func withPatchedStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	origStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = origStdin })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	fn()
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	origStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = origStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String()
}
