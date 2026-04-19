package ctlcmd

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/ui/style"
)

func runContactInvite(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando contact invite <start|accept> [flags]")
	}
	switch args[0] {
	case "start":
		return runContactInviteStart(args[1:])
	case "accept":
		return runContactInviteAccept(args[1:])
	default:
		return fmt.Errorf("unknown contact invite subcommand %q", args[0])
	}
}

func runContactInviteStart(args []string) error {
	bfs := NewBaseFlagSet("contact invite start")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	code := bfs.FS.String("code", "", "optional invite code override")
	timeout := bfs.FS.Duration("timeout", 2*time.Minute, "how long to wait for the other person")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	generatedCode := *code
	if generatedCode == "" {
		var err error
		generatedCode, err = generateInviteCode()
		if err != nil {
			return err
		}
	}
	fmt.Printf("invite code: %s\n", generatedCode)
	fmt.Println("tell the other person to run: pando contact invite accept --mailbox <their-mailbox> --code <invite-code>")
	return runInviteExchange(*bfs.RootDir, *bfs.DataDir, *bfs.Mailbox, *relayURL, *relayToken, generatedCode, *timeout)
}

func runContactInviteAccept(args []string) error {
	bfs := NewBaseFlagSet("contact invite accept")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	code := bfs.FS.String("code", "", "invite code")
	timeout := bfs.FS.Duration("timeout", 2*time.Minute, "how long to wait for the other person")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*code) == "" {
		return fmt.Errorf("-code is required")
	}
	return runInviteExchange(*bfs.RootDir, *bfs.DataDir, *bfs.Mailbox, *relayURL, *relayToken, *code, *timeout)
}

func runInviteExchange(rootDir, dataDir, mailbox, relayURL, relayToken, code string, timeout time.Duration) error {
	resolvedMailbox, resolvedDataDir, err := resolveDataDirWithRoot(rootDir, dataDir, mailbox)
	if err != nil {
		return err
	}
	resolvedRelayURL, resolvedRelayToken, err := resolveRelayConfig(rootDir, relayURL, relayToken)
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(resolvedMailbox)
	if err != nil {
		return err
	}
	client, err := relayapi.NewClient(resolvedRelayURL, resolvedRelayToken)
	if err != nil {
		return err
	}
	rendezvousID := deriveRendezvousID(code)
	payload, err := encryptInviteBundle(code, id.InviteBundle())
	if err != nil {
		return err
	}
	if err := client.PutRendezvousPayload(rendezvousID, payload); err != nil {
		return err
	}
	deadline := time.Now().UTC().Add(timeout)
	for time.Now().UTC().Before(deadline) {
		payloads, err := client.GetRendezvousPayloads(rendezvousID)
		if err != nil {
			return err
		}
		for _, candidate := range payloads {
			bundle, err := decryptInviteBundle(code, candidate)
			if err != nil {
				continue
			}
			if bundle.AccountID == id.AccountID {
				continue
			}
			contact, err := saveTrustedInviteContact(clientStore, *bundle)
			if err != nil {
				return err
			}
			fmt.Printf("added invite contact %s with %d active devices\n", contact.AccountID, len(contact.ActiveDevices()))
			fmt.Printf("fingerprint: %s\n", style.FormatFingerprint(contact.Fingerprint()))
			return nil
		}
		time.Sleep(750 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for the other person to complete the invite exchange")
}

func saveTrustedInviteContact(clientStore *store.ClientStore, bundle identity.InviteBundle) (*identity.Contact, error) {
	contact, err := identity.ContactFromInvite(bundle)
	if err != nil {
		return nil, err
	}
	if existing, loadErr := clientStore.LoadContact(contact.AccountID); loadErr == nil && existing.Fingerprint() == contact.Fingerprint() {
		contact.Verified = existing.Verified
		contact.TrustSource = existing.TrustSource
	}
	contact.Verified = true
	contact.TrustSource = identity.StrongerTrust(contact.TrustSource, identity.TrustSourceInviteCode)
	contact.NormalizeTrust()
	if err := clientStore.SaveContact(contact); err != nil {
		return nil, err
	}
	return contact, nil
}

func resolveDataDirWithRoot(rootDir, dataDir, mailbox string) (string, string, error) {
	resolvedMailbox := mailbox
	if strings.TrimSpace(resolvedMailbox) == "" {
		devCfg, err := config.LoadDeviceConfig(rootDir)
		if err != nil {
			return "", "", err
		}
		resolvedMailbox = devCfg.DefaultMailbox
	}
	if strings.TrimSpace(resolvedMailbox) == "" {
		return "", "", fmt.Errorf("-mailbox is required")
	}
	resolvedDataDir, err := resolveDataDir(resolvedMailbox, rootDir, dataDir)
	if err != nil {
		return "", "", err
	}
	return resolvedMailbox, resolvedDataDir, nil
}

func generateInviteCode() (string, error) {
	digits := make([]byte, 10)
	random := make([]byte, len(digits))
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate invite code: %w", err)
	}
	for idx := range digits {
		digits[idx] = '0' + (random[idx] % 10)
	}
	return fmt.Sprintf("%s-%s", digits[:5], digits[5:]), nil
}

func deriveRendezvousID(code string) string {
	normalized := normalizeInviteCode(code)
	hash := sha256.Sum256([]byte("pando-rendezvous-id-v1\n" + normalized))
	return base64.RawURLEncoding.EncodeToString(hash[:16])
}

func encryptInviteBundle(code string, bundle identity.InviteBundle) (relayapi.RendezvousPayload, error) {
	key, err := deriveInviteKey(code)
	if err != nil {
		return relayapi.RendezvousPayload{}, err
	}
	aead, err := newInviteAEAD(key)
	if err != nil {
		return relayapi.RendezvousPayload{}, fmt.Errorf("create rendezvous cipher: %w", err)
	}
	plaintext, err := json.Marshal(bundle)
	if err != nil {
		return relayapi.RendezvousPayload{}, fmt.Errorf("encode invite bundle: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return relayapi.RendezvousPayload{}, fmt.Errorf("generate rendezvous nonce: %w", err)
	}
	now := time.Now().UTC()
	return relayapi.RendezvousPayload{
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, nil)),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		CreatedAt:  now,
		ExpiresAt:  now.Add(10 * time.Minute),
	}, nil
}

func decryptInviteBundle(code string, payload relayapi.RendezvousPayload) (*identity.InviteBundle, error) {
	key, err := deriveInviteKey(code)
	if err != nil {
		return nil, err
	}
	aead, err := newInviteAEAD(key)
	if err != nil {
		return nil, fmt.Errorf("create rendezvous cipher: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(payload.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode rendezvous nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode rendezvous ciphertext: %w", err)
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt rendezvous payload: %w", err)
	}
	var bundle identity.InviteBundle
	if err := json.Unmarshal(plaintext, &bundle); err != nil {
		return nil, fmt.Errorf("decode invite bundle: %w", err)
	}
	return &bundle, nil
}

func deriveInviteKey(code string) ([]byte, error) {
	hash := sha256.Sum256([]byte("pando-rendezvous-v1\n" + normalizeInviteCode(code)))
	return append([]byte(nil), hash[:]...), nil
}

func newInviteAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func normalizeInviteCode(code string) string {
	code = strings.TrimSpace(strings.ToLower(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}
