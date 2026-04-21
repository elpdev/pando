package ctlcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/invite"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relay"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
	wsclient "github.com/elpdev/pando/internal/transport/ws"
	"rsc.io/qr"
)

const testPassphrase = "test-passphrase"

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
	code, err := invite.EncodeCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	bundle, err := invite.DecodeCode(code)
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
	if got := invite.ExtractCode(text); got != "raw-invite-code" {
		t.Fatalf("expected invite code extraction, got %q", got)
	}
}

func TestDecodeInviteTextAcceptsVerboseInviteOutput(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := invite.EncodeCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	bundle, err := invite.DecodeText("account: alice\nfingerprint: " + id.Fingerprint() + "\ninvite-code: " + code + "\n")
	if err != nil {
		t.Fatalf("decode verbose invite text: %v", err)
	}
	if bundle.AccountID != "alice" {
		t.Fatalf("expected alice bundle, got %q", bundle.AccountID)
	}
}

func TestRunImportContactWithStdin(t *testing.T) {
	withTestPassphrase(t)
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
	protectTestStore(t, aliceStore)
	protectTestStore(t, bobStore)
	code, err := invite.EncodeCode(aliceID.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}

	withPatchedStdin(t, code+"\n", func() {
		if err := runImportContactWithName("contact add", []string{"-mailbox", "bob", "-data-dir", bobDir, "-stdin"}); err != nil {
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
	if !contact.Verified {
		t.Fatalf("expected contact add to verify imported contact")
	}
}

func TestRunImportContactWithPaste(t *testing.T) {
	withTestPassphrase(t)
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
	protectTestStore(t, aliceStore)
	protectTestStore(t, bobStore)
	code, err := invite.EncodeCode(aliceID.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	pasted := "account: alice\nfingerprint: " + aliceID.Fingerprint() + "\ninvite-code: " + code + "\n"

	withPatchedStdin(t, pasted, func() {
		if err := runImportContactWithName("contact add", []string{"-mailbox", "bob", "-data-dir", bobDir, "-paste"}); err != nil {
			t.Fatalf("import contact from paste: %v", err)
		}
	})

	if _, err := bobStore.LoadContact("alice"); err != nil {
		t.Fatalf("load imported alice contact: %v", err)
	}
}

func TestRunImportContactLeavesContactUnverified(t *testing.T) {
	withTestPassphrase(t)
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
	protectTestStore(t, aliceStore)
	protectTestStore(t, bobStore)
	code, err := invite.EncodeCode(aliceID.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}

	withPatchedStdin(t, code+"\n", func() {
		if err := runImportContactWithName("contact import", []string{"-mailbox", "bob", "-data-dir", bobDir, "-stdin"}); err != nil {
			t.Fatalf("import contact from stdin: %v", err)
		}
	})

	contact, err := bobStore.LoadContact("alice")
	if err != nil {
		t.Fatalf("load alice contact: %v", err)
	}
	if contact.Verified {
		t.Fatalf("expected contact import to leave contact unverified")
	}
}

func TestRunAddContactOutputShowsVerification(t *testing.T) {
	withTestPassphrase(t)
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
	protectTestStore(t, aliceStore)
	protectTestStore(t, bobStore)
	code, err := invite.EncodeCode(aliceID.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}

	output := captureStdout(t, func() {
		withPatchedStdin(t, code+"\n", func() {
			if err := runImportContactWithName("contact add", []string{"-mailbox", "bob", "-data-dir", bobDir, "-stdin"}); err != nil {
				t.Fatalf("add contact from stdin: %v", err)
			}
		})
	})
	if !strings.Contains(output, "verified contact alice (") {
		t.Fatalf("expected verification confirmation in output, got %q", output)
	}
	if strings.Contains(output, "next: pando contact verify") {
		t.Fatalf("expected no manual verify step in output, got %q", output)
	}
}

func TestRunInviteCodeRaw(t *testing.T) {
	withTestPassphrase(t)
	dataDir := t.TempDir()
	clientStore := store.NewClientStore(dataDir)
	id, _, err := clientStore.LoadOrCreateIdentity("alice")
	if err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	protectTestStore(t, clientStore)
	code, err := invite.EncodeCode(id.InviteBundle())
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
	withTestPassphrase(t)
	dataDir := t.TempDir()
	clientStore := store.NewClientStore(dataDir)
	if _, _, err := clientStore.LoadOrCreateIdentity("alice"); err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	protectTestStore(t, clientStore)

	output := captureStdout(t, func() {
		if err := runInviteCode([]string{"-mailbox", "alice", "-data-dir", dataDir}); err != nil {
			t.Fatalf("run invite code: %v", err)
		}
	})
	if !strings.Contains(output, "the other person can import it with: pando contact add --mailbox <their-mailbox> --paste") {
		t.Fatalf("expected next-step guidance in output, got %q", output)
	}
}

func TestExecuteGroupedIdentityInit(t *testing.T) {
	withTestPassphrase(t)
	dataDir := t.TempDir()
	output := captureStdout(t, func() {
		if err := Execute([]string{"identity", "init", "-mailbox", "alice", "-data-dir", dataDir}); err != nil {
			t.Fatalf("execute identity init: %v", err)
		}
	})
	if !strings.Contains(output, "initialized identity for alice on device alice") && !strings.Contains(output, "identity already exists for alice on device alice") {
		t.Fatalf("unexpected identity init output: %q", output)
	}
}

func TestIdentityInitCanPublishDirectoryEntry(t *testing.T) {
	withTestPassphrase(t)
	serverURL := newRelayTestServer(t)
	dataDir := t.TempDir()
	output := captureStdout(t, func() {
		if err := runInit([]string{"-mailbox", "alice", "-data-dir", dataDir, "-publish-directory", "-relay", serverURL, "-relay-token", "secret"}); err != nil {
			t.Fatalf("run identity init with publish: %v", err)
		}
	})
	if !strings.Contains(output, "published trusted relay directory entry for alice") {
		t.Fatalf("expected publish confirmation, got %q", output)
	}
	client, err := relayapi.NewClient(serverURL, "secret")
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	entry, err := client.LookupDirectoryEntry("alice")
	if err != nil {
		t.Fatalf("lookup directory entry: %v", err)
	}
	if entry.Entry.Mailbox != "alice" {
		t.Fatalf("expected alice directory entry, got %+v", entry)
	}
}

func TestRunChangePassphraseUsesNewEnvironmentVariable(t *testing.T) {
	withTestPassphrase(t)
	dataDir := t.TempDir()
	clientStore := store.NewClientStore(dataDir)
	if _, _, err := clientStore.LoadOrCreateIdentity("alice"); err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	protectTestStore(t, clientStore)
	t.Setenv("PANDO_PASSPHRASE_NEW", "rotated-passphrase")
	output := captureStdout(t, func() {
		if err := runChangePassphrase([]string{"-mailbox", "alice", "-data-dir", dataDir}); err != nil {
			t.Fatalf("run change passphrase: %v", err)
		}
	})
	if !strings.Contains(output, "updated passphrase for alice") {
		t.Fatalf("expected passphrase change output, got %q", output)
	}
	reopened := store.NewClientStore(dataDir)
	if err := reopened.UsePassphrase([]byte("rotated-passphrase")); err != nil {
		t.Fatalf("unlock store with rotated passphrase: %v", err)
	}
	if _, err := reopened.LoadIdentity(); err != nil {
		t.Fatalf("load identity after passphrase rotation: %v", err)
	}
}

func TestExecuteGroupedConfigSetRelayToken(t *testing.T) {
	rootDir := t.TempDir()
	if err := Execute([]string{"config", "set", "relay-token", "-root-dir", rootDir, "secret-token"}); err != nil {
		t.Fatalf("execute config set relay-token: %v", err)
	}
	bytes, err := os.ReadFile(filepath.Join(rootDir, "config.yml"))
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(bytes), "relay_token: secret-token") {
		t.Fatalf("expected relay token in config file, got %q", string(bytes))
	}
}

func TestReadInviteBundleFromQRImage(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := invite.EncodeCode(id.InviteBundle())
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

func TestRunConfigSetRelayTokenAndShow(t *testing.T) {
	rootDir := t.TempDir()

	setOutput := captureStdout(t, func() {
		if err := runConfigSetRelayToken([]string{"-root-dir", rootDir, "secret-token"}); err != nil {
			t.Fatalf("set relay token: %v", err)
		}
	})
	if !strings.Contains(setOutput, "relay_token set to secret-token") {
		t.Fatalf("expected relay token confirmation, got %q", setOutput)
	}

	showOutput := captureStdout(t, func() {
		if err := runConfigShow([]string{"-root-dir", rootDir}); err != nil {
			t.Fatalf("show config: %v", err)
		}
	})
	if !strings.Contains(showOutput, "relay_token: secret-token") {
		t.Fatalf("expected relay token in config output, got %q", showOutput)
	}

	bytes, err := os.ReadFile(filepath.Join(rootDir, "config.yml"))
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(bytes), "relay_token: secret-token") {
		t.Fatalf("expected relay token in config file, got %q", string(bytes))
	}
}

func TestPublishDirectoryAndLookupContact(t *testing.T) {
	withTestPassphrase(t)
	serverURL := newRelayTestServer(t)
	aliceDir := t.TempDir()
	bobDir := t.TempDir()
	aliceStore := store.NewClientStore(aliceDir)
	bobStore := store.NewClientStore(bobDir)
	if _, _, err := aliceStore.LoadOrCreateIdentity("alice"); err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	if _, _, err := bobStore.LoadOrCreateIdentity("bob"); err != nil {
		t.Fatalf("create bob identity: %v", err)
	}
	protectTestStore(t, aliceStore)
	protectTestStore(t, bobStore)
	if err := runPublishDirectory([]string{"-mailbox", "alice", "-data-dir", aliceDir, "-relay", serverURL, "-relay-token", "secret", "-discoverable"}); err != nil {
		t.Fatalf("publish directory: %v", err)
	}
	if err := runLookupContact([]string{"-mailbox", "bob", "-data-dir", bobDir, "-contact", "alice", "-relay", serverURL, "-relay-token", "secret"}); err != nil {
		t.Fatalf("lookup contact: %v", err)
	}
	contact, err := bobStore.LoadContact("alice")
	if err != nil {
		t.Fatalf("load relay directory contact: %v", err)
	}
	if !contact.Verified {
		t.Fatal("expected relay directory contact to be verified")
	}
	if contact.TrustSource != identity.TrustSourceRelayDirectory {
		t.Fatalf("expected relay trust source, got %q", contact.TrustSource)
	}
}

func TestDiscoverContactsListsOnlyDiscoverableEntries(t *testing.T) {
	withTestPassphrase(t)
	serverURL := newRelayTestServer(t)
	aliceDir := t.TempDir()
	bobDir := t.TempDir()
	aliceStore := store.NewClientStore(aliceDir)
	if _, _, err := aliceStore.LoadOrCreateIdentity("alice"); err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	bobStore := store.NewClientStore(bobDir)
	if _, _, err := bobStore.LoadOrCreateIdentity("bob"); err != nil {
		t.Fatalf("create bob identity: %v", err)
	}
	protectTestStore(t, aliceStore)
	protectTestStore(t, bobStore)
	if err := runPublishDirectory([]string{"-mailbox", "alice", "-data-dir", aliceDir, "-relay", serverURL, "-relay-token", "secret", "-discoverable"}); err != nil {
		t.Fatalf("publish discoverable alice directory: %v", err)
	}
	if err := runPublishDirectory([]string{"-mailbox", "bob", "-data-dir", bobDir, "-relay", serverURL, "-relay-token", "secret"}); err != nil {
		t.Fatalf("publish bob directory: %v", err)
	}
	output := captureStdout(t, func() {
		if err := runDiscoverContacts([]string{"-relay", serverURL, "-relay-token", "secret"}); err != nil {
			t.Fatalf("discover contacts: %v", err)
		}
	})
	var entries []struct {
		Mailbox string `json:"mailbox"`
	}
	if err := json.Unmarshal([]byte(output), &entries); err != nil {
		t.Fatalf("decode discover output: %v", err)
	}
	if len(entries) != 1 || entries[0].Mailbox != "alice" {
		t.Fatalf("expected only discoverable alice entry, got %+v", entries)
	}
}

func TestRequestAcceptAndRejectContactFlow(t *testing.T) {
	withTestPassphrase(t)
	serverURL := newRelayTestServer(t)
	aliceDir := t.TempDir()
	bobDir := t.TempDir()
	aliceStore := store.NewClientStore(aliceDir)
	if _, _, err := aliceStore.LoadOrCreateIdentity("alice"); err != nil {
		t.Fatalf("create alice identity: %v", err)
	}
	bobStore := store.NewClientStore(bobDir)
	if _, _, err := bobStore.LoadOrCreateIdentity("bob"); err != nil {
		t.Fatalf("create bob identity: %v", err)
	}
	protectTestStore(t, aliceStore)
	protectTestStore(t, bobStore)
	if err := runPublishDirectory([]string{"-mailbox", "alice", "-data-dir", aliceDir, "-relay", serverURL, "-relay-token", "secret", "-discoverable"}); err != nil {
		t.Fatalf("publish discoverable alice directory: %v", err)
	}
	if err := runPublishDirectory([]string{"-mailbox", "bob", "-data-dir", bobDir, "-relay", serverURL, "-relay-token", "secret", "-discoverable"}); err != nil {
		t.Fatalf("publish discoverable bob directory: %v", err)
	}
	if err := runRequestContact([]string{"-mailbox", "alice", "-data-dir", aliceDir, "-contact", "bob", "-relay", serverURL, "-relay-token", "secret", "-note", "hi bob"}); err != nil {
		t.Fatalf("request contact: %v", err)
	}
	bobResult := receiveNextControlResult(t, serverURL, bobDir, "bob")
	if bobResult.ContactRequest == nil || bobResult.ContactRequest.AccountID != "alice" || bobResult.ContactRequest.Note != "hi bob" {
		t.Fatalf("unexpected bob request result: %+v", bobResult)
	}
	bobRequestsOutput := captureStdout(t, func() {
		if err := runListContactRequests([]string{"-mailbox", "bob", "-data-dir", bobDir}); err != nil {
			t.Fatalf("list bob contact requests: %v", err)
		}
	})
	if !strings.Contains(bobRequestsOutput, "alice") || !strings.Contains(bobRequestsOutput, "pending") {
		t.Fatalf("expected pending alice request in output, got %q", bobRequestsOutput)
	}
	if err := runAcceptContactRequest([]string{"-mailbox", "bob", "-data-dir", bobDir, "-contact", "alice", "-relay", serverURL, "-relay-token", "secret"}); err != nil {
		t.Fatalf("accept contact request: %v", err)
	}
	aliceResult := receiveNextControlResult(t, serverURL, aliceDir, "alice")
	if aliceResult.ContactRequest == nil || aliceResult.ContactRequest.Status != store.ContactRequestStatusAccepted {
		t.Fatalf("unexpected alice accepted request result: %+v", aliceResult)
	}
	aliceContact, err := unlockedTestStore(t, aliceDir).LoadContact("bob")
	if err != nil {
		t.Fatalf("load accepted bob contact: %v", err)
	}
	if aliceContact.AccountID != "bob" {
		t.Fatalf("unexpected accepted contact: %+v", aliceContact)
	}

	carolDir := t.TempDir()
	carolStore := store.NewClientStore(carolDir)
	if _, _, err := carolStore.LoadOrCreateIdentity("carol"); err != nil {
		t.Fatalf("create carol identity: %v", err)
	}
	protectTestStore(t, carolStore)
	if err := runPublishDirectory([]string{"-mailbox", "carol", "-data-dir", carolDir, "-relay", serverURL, "-relay-token", "secret", "-discoverable"}); err != nil {
		t.Fatalf("publish discoverable carol directory: %v", err)
	}
	if err := runRequestContact([]string{"-mailbox", "alice", "-data-dir", aliceDir, "-contact", "carol", "-relay", serverURL, "-relay-token", "secret"}); err != nil {
		t.Fatalf("request carol contact: %v", err)
	}
	carolResult := receiveNextControlResult(t, serverURL, carolDir, "carol")
	if carolResult.ContactRequest == nil || carolResult.ContactRequest.AccountID != "alice" {
		t.Fatalf("unexpected carol request result: %+v", carolResult)
	}
	if err := runRejectContactRequest([]string{"-mailbox", "carol", "-data-dir", carolDir, "-contact", "alice", "-relay", serverURL, "-relay-token", "secret"}); err != nil {
		t.Fatalf("reject contact request: %v", err)
	}
	aliceRejectResult := receiveNextControlResult(t, serverURL, aliceDir, "alice")
	if aliceRejectResult.ContactRequest == nil || aliceRejectResult.ContactRequest.Status != store.ContactRequestStatusRejected {
		t.Fatalf("unexpected alice rejected request result: %+v", aliceRejectResult)
	}
	if _, err := unlockedTestStore(t, aliceDir).LoadContact("carol"); err != store.ErrNotFound {
		t.Fatalf("expected no carol contact after rejection, got %v", err)
	}
}

func TestContactInviteExchangeAddsTrustedContacts(t *testing.T) {
	withTestPassphrase(t)
	serverURL := newRelayTestServer(t)
	aliceDir := t.TempDir()
	bobDir := t.TempDir()
	errs := make(chan error, 1)
	go func() {
		errs <- runContactInviteStart([]string{"-mailbox", "alice", "-data-dir", aliceDir, "-relay", serverURL, "-relay-token", "secret", "-code", "12345-67890", "-timeout", "5s"})
	}()
	time.Sleep(200 * time.Millisecond)
	if err := runContactInviteAccept([]string{"-mailbox", "bob", "-data-dir", bobDir, "-relay", serverURL, "-relay-token", "secret", "-code", "12345-67890", "-timeout", "5s"}); err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if err := <-errs; err != nil {
		t.Fatalf("start invite: %v", err)
	}
	aliceContact, err := unlockedTestStore(t, aliceDir).LoadContact("bob")
	if err != nil {
		t.Fatalf("load alice contact: %v", err)
	}
	bobContact, err := unlockedTestStore(t, bobDir).LoadContact("alice")
	if err != nil {
		t.Fatalf("load bob contact: %v", err)
	}
	if !aliceContact.Verified || aliceContact.TrustSource != identity.TrustSourceInviteCode {
		t.Fatalf("unexpected alice invite contact state: %+v", aliceContact)
	}
	if !bobContact.Verified || bobContact.TrustSource != identity.TrustSourceInviteCode {
		t.Fatalf("unexpected bob invite contact state: %+v", bobContact)
	}
}

func newRelayTestServer(t *testing.T) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(relay.NewServer(logger, relay.NewMemoryQueueStore(), relay.Options{AuthToken: "secret"}).Handler())
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
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

func receiveNextControlResult(t *testing.T, serverURL, dataDir, mailbox string) *messaging.IncomingResult {
	t.Helper()
	clientStore := store.NewClientStore(dataDir)
	protectTestStore(t, clientStore)
	service, _, err := messaging.New(clientStore, mailbox)
	if err != nil {
		t.Fatalf("new messaging service for %s: %v", mailbox, err)
	}
	directoryClient, err := relayapi.NewClient(serverURL, "secret")
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	service.SetDirectoryClient(directoryClient)
	client := wsclient.NewClient(serverURL, "secret", service.Identity())
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("connect relay client for %s: %v", mailbox, err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-client.Events():
			if event.Err != nil {
				t.Fatalf("relay event error for %s: %v", mailbox, event.Err)
			}
			if event.Message == nil || event.Message.Type != protocol.MessageTypeIncoming || event.Message.Incoming == nil {
				continue
			}
			result, err := service.HandleIncoming(*event.Message.Incoming)
			if err != nil {
				t.Fatalf("handle incoming envelope for %s: %v", mailbox, err)
			}
			if result != nil && result.Control {
				return result
			}
		case <-deadline:
			t.Fatalf("timed out waiting for control result for %s", mailbox)
		}
	}
}

func withTestPassphrase(t *testing.T) {
	t.Helper()
	t.Setenv("PANDO_PASSPHRASE", testPassphrase)
}

func protectTestStore(t *testing.T, clientStore *store.ClientStore) {
	t.Helper()
	if err := clientStore.UsePassphrase([]byte(testPassphrase)); err != nil {
		t.Fatalf("protect test store: %v", err)
	}
}

func unlockedTestStore(t *testing.T, dataDir string) *store.ClientStore {
	t.Helper()
	clientStore := store.NewClientStore(dataDir)
	protectTestStore(t, clientStore)
	return clientStore
}
