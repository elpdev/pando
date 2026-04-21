package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/identity"
)

const (
	protectedFileKind       = "pando-protected-v1"
	protectedFileVersion    = 1
	protectedKeyBytes       = 32
	protectedPBKDF2Rounds   = 600000
	protectedFileSaltBytes  = 16
	protectedFileNonceBytes = 12
)

var ErrUnsupportedProtectedVersion = errors.New("unsupported protected file version")
var ErrMalformedProtectedFile = errors.New("malformed protected file")

type protectedFileEnvelope struct {
	Kind       string `json:"kind"`
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	Iterations uint32 `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type protectedFileState int

const (
	protectedFileMissing protectedFileState = iota
	protectedFilePlaintext
	protectedFileEncrypted
)

func isProtectedEnvelope(bytes []byte) bool {
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(bytes, &probe); err != nil {
		return false
	}
	return probe.Kind == protectedFileKind
}

func encryptProtectedPayload(passphrase, plaintext []byte) ([]byte, error) {
	salt := make([]byte, protectedFileSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate protected salt: %w", err)
	}
	key, err := deriveProtectedFileKey(passphrase, salt, protectedPBKDF2Rounds)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create protected cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create protected AEAD: %w", err)
	}
	nonce := make([]byte, protectedFileNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate protected nonce: %w", err)
	}
	envelope := protectedFileEnvelope{
		Kind:       protectedFileKind,
		Version:    protectedFileVersion,
		KDF:        "pbkdf2-sha256",
		Iterations: protectedPBKDF2Rounds,
		Salt:       base64.RawStdEncoding.EncodeToString(salt),
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(gcm.Seal(nil, nonce, plaintext, nil)),
	}
	bytes, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode protected payload: %w", err)
	}
	return bytes, nil
}

func decryptProtectedPayload(passphrase, bytes []byte) ([]byte, error) {
	var envelope protectedFileEnvelope
	if err := json.Unmarshal(bytes, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decode envelope: %v", ErrMalformedProtectedFile, err)
	}
	if envelope.Kind != protectedFileKind {
		return nil, fmt.Errorf("%w: missing kind marker", ErrMalformedProtectedFile)
	}
	if envelope.Version != protectedFileVersion {
		return nil, fmt.Errorf("%w: version %d", ErrUnsupportedProtectedVersion, envelope.Version)
	}
	if envelope.KDF != "pbkdf2-sha256" {
		return nil, fmt.Errorf("%w: unsupported kdf %q", ErrMalformedProtectedFile, envelope.KDF)
	}
	salt, err := base64.RawStdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return nil, fmt.Errorf("%w: decode salt: %v", ErrMalformedProtectedFile, err)
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("%w: decode nonce: %v", ErrMalformedProtectedFile, err)
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: decode ciphertext: %v", ErrMalformedProtectedFile, err)
	}
	key, err := deriveProtectedFileKey(passphrase, salt, envelope.Iterations)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create protected cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create protected AEAD: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrInvalidPassphrase
	}
	return plaintext, nil
}

func deriveProtectedFileKey(passphrase, salt []byte, iterations uint32) ([]byte, error) {
	key, err := pbkdf2.Key(sha256.New, string(passphrase), salt, int(iterations), protectedKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("derive protected key: %w", err)
	}
	return key, nil
}

func (s *ClientStore) protectedPaths() []string {
	return []string{s.identityPath(), s.contactsPath(), s.pendingEnrollmentPath()}
}

func (s *ClientStore) protectedFileCounts() (int, int, error) {
	plain := 0
	encrypted := 0
	for _, path := range s.protectedPaths() {
		state, err := protectedStateForPath(path)
		if err != nil {
			return 0, 0, err
		}
		switch state {
		case protectedFilePlaintext:
			plain++
		case protectedFileEncrypted:
			encrypted++
		}
	}
	return plain, encrypted, nil
}

func protectedStateForPath(path string) (protectedFileState, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return protectedFileMissing, nil
		}
		return protectedFileMissing, fmt.Errorf("read %s: %w", path, err)
	}
	if isProtectedEnvelope(bytes) {
		return protectedFileEncrypted, nil
	}
	return protectedFilePlaintext, nil
}

func (s *ClientStore) validatePassphrase(passphrase []byte) error {
	for _, path := range s.protectedPaths() {
		state, err := protectedStateForPath(path)
		if err != nil {
			return err
		}
		if state != protectedFileEncrypted {
			continue
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if _, err := decryptProtectedPayload(passphrase, bytes); err != nil {
			return fmt.Errorf("unlock %s: %w", path, err)
		}
		return nil
	}
	return nil
}

func (s *ClientStore) migrateProtectedFiles() error {
	if len(s.passphrase) == 0 {
		return ErrPassphraseRequired
	}
	if err := s.migrateProtectedIdentity(); err != nil {
		return err
	}
	if err := s.migrateProtectedContacts(); err != nil {
		return err
	}
	if err := s.migrateProtectedPendingEnrollment(); err != nil {
		return err
	}
	return nil
}

func (s *ClientStore) migrateProtectedIdentity() error {
	state, err := protectedStateForPath(s.identityPath())
	if err != nil || state != protectedFilePlaintext {
		return err
	}
	var id identity.Identity
	if err := s.readJSON(s.identityPath(), &id); err != nil {
		return err
	}
	return s.writeProtectedJSON(s.identityPath(), &id, 0o600)
}

func (s *ClientStore) migrateProtectedContacts() error {
	state, err := protectedStateForPath(s.contactsPath())
	if err != nil || state != protectedFilePlaintext {
		return err
	}
	contacts := make(map[string]identity.Contact)
	if err := s.readJSON(s.contactsPath(), &contacts); err != nil {
		return err
	}
	return s.writeProtectedJSON(s.contactsPath(), contacts, 0o600)
}

func (s *ClientStore) migrateProtectedPendingEnrollment() error {
	state, err := protectedStateForPath(s.pendingEnrollmentPath())
	if err != nil || state != protectedFilePlaintext {
		return err
	}
	var pending identity.PendingEnrollment
	if err := s.readJSON(s.pendingEnrollmentPath(), &pending); err != nil {
		return err
	}
	return s.writeProtectedJSON(s.pendingEnrollmentPath(), &pending, 0o600)
}
