package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/nacl/box"
)

type Identity struct {
	AccountID             string             `json:"account_id"`
	CurrentDeviceID       string             `json:"current_device_id"`
	AccountSigningPublic  ed25519.PublicKey  `json:"account_signing_public"`
	AccountSigningPrivate ed25519.PrivateKey `json:"account_signing_private"`
	Devices               []Device           `json:"devices"`
}

type Device struct {
	ID                string             `json:"id"`
	Mailbox           string             `json:"mailbox"`
	SigningPublic     ed25519.PublicKey  `json:"signing_public"`
	SigningPrivate    ed25519.PrivateKey `json:"signing_private,omitempty"`
	EncryptionPublic  []byte             `json:"encryption_public"`
	EncryptionPrivate []byte             `json:"encryption_private,omitempty"`
	Signature         []byte             `json:"signature"`
	Revoked           bool               `json:"revoked"`
	RevokedAt         time.Time          `json:"revoked_at,omitempty"`
}

type DeviceBundle struct {
	ID               string            `json:"id"`
	Mailbox          string            `json:"mailbox"`
	SigningPublic    ed25519.PublicKey `json:"signing_public"`
	EncryptionPublic []byte            `json:"encryption_public"`
	Signature        []byte            `json:"signature"`
	Revoked          bool              `json:"revoked"`
	RevokedAt        time.Time         `json:"revoked_at,omitempty"`
}

type InviteBundle struct {
	AccountID            string            `json:"account_id"`
	AccountSigningPublic ed25519.PublicKey `json:"account_signing_public"`
	Devices              []DeviceBundle    `json:"devices"`
}

type Contact struct {
	AccountID            string            `json:"account_id"`
	AccountSigningPublic ed25519.PublicKey `json:"account_signing_public"`
	Devices              []ContactDevice   `json:"devices"`
	Verified             bool              `json:"verified"`
}

type ContactDevice struct {
	ID               string            `json:"id"`
	Mailbox          string            `json:"mailbox"`
	SigningPublic    ed25519.PublicKey `json:"signing_public"`
	EncryptionPublic []byte            `json:"encryption_public"`
	Revoked          bool              `json:"revoked"`
	RevokedAt        time.Time         `json:"revoked_at,omitempty"`
}

type PendingEnrollment struct {
	AccountID string `json:"account_id"`
	Device    Device `json:"device"`
}

type EnrollmentRequest struct {
	AccountID string `json:"account_id"`
	DeviceID  string `json:"device_id"`
	Mailbox   string `json:"mailbox"`
	Device    Device `json:"device"`
}

type EnrollmentApproval struct {
	AccountID  string `json:"account_id"`
	Ciphertext string `json:"ciphertext"`
}

type approvalPayload struct {
	AccountID             string             `json:"account_id"`
	AccountSigningPublic  ed25519.PublicKey  `json:"account_signing_public"`
	AccountSigningPrivate ed25519.PrivateKey `json:"account_signing_private"`
	Devices               []DeviceBundle     `json:"devices"`
	CurrentDeviceID       string             `json:"current_device_id"`
}

func New(mailbox string) (*Identity, error) {
	return NewWithAccount(mailbox, mailbox)
}

func NewWithAccount(accountID, mailbox string) (*Identity, error) {
	accountPub, accountPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate account signing key: %w", err)
	}
	device, err := newDevice(mailbox)
	if err != nil {
		return nil, err
	}
	device.Signature = ed25519.Sign(accountPriv, deviceStatement(accountID, device.ID, device.Mailbox, device.SigningPublic, device.EncryptionPublic))

	return &Identity{
		AccountID:             accountID,
		CurrentDeviceID:       device.ID,
		AccountSigningPublic:  accountPub,
		AccountSigningPrivate: accountPriv,
		Devices:               []Device{device},
	}, nil
}

func NewPendingEnrollment(accountID, mailbox string) (*PendingEnrollment, error) {
	device, err := newDevice(mailbox)
	if err != nil {
		return nil, err
	}
	return &PendingEnrollment{AccountID: accountID, Device: device}, nil
}

func (p *PendingEnrollment) Request() EnrollmentRequest {
	return EnrollmentRequest{AccountID: p.AccountID, DeviceID: p.Device.ID, Mailbox: p.Device.Mailbox, Device: p.Device.publicOnly()}
}

func (p *PendingEnrollment) Complete(approval EnrollmentApproval) (*Identity, error) {
	if approval.AccountID != p.AccountID {
		return nil, fmt.Errorf("approval account mismatch")
	}
	publicKey, privateKey, err := p.Device.EncryptionKeyPair()
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(approval.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode approval ciphertext: %w", err)
	}
	plaintext, ok := box.OpenAnonymous(nil, ciphertext, publicKey, privateKey)
	if !ok {
		return nil, fmt.Errorf("decrypt approval payload")
	}
	var payload approvalPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("decode approval payload: %w", err)
	}
	if payload.CurrentDeviceID != p.Device.ID {
		return nil, fmt.Errorf("approval does not target this device")
	}
	id := &Identity{
		AccountID:             payload.AccountID,
		CurrentDeviceID:       p.Device.ID,
		AccountSigningPublic:  payload.AccountSigningPublic,
		AccountSigningPrivate: payload.AccountSigningPrivate,
		Devices:               make([]Device, 0, len(payload.Devices)),
	}
	for _, bundle := range payload.Devices {
		device := Device{
			ID:               bundle.ID,
			Mailbox:          bundle.Mailbox,
			SigningPublic:    append(ed25519.PublicKey(nil), bundle.SigningPublic...),
			EncryptionPublic: append([]byte(nil), bundle.EncryptionPublic...),
			Signature:        append([]byte(nil), bundle.Signature...),
			Revoked:          bundle.Revoked,
			RevokedAt:        bundle.RevokedAt,
		}
		if device.ID == p.Device.ID {
			device.SigningPrivate = append(ed25519.PrivateKey(nil), p.Device.SigningPrivate...)
			device.EncryptionPrivate = append([]byte(nil), p.Device.EncryptionPrivate...)
		}
		id.Devices = append(id.Devices, device)
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return id, nil
}

func (i *Identity) InviteBundle() InviteBundle {
	bundles := make([]DeviceBundle, 0, len(i.Devices))
	for _, device := range i.Devices {
		if device.Revoked {
			continue
		}
		bundles = append(bundles, device.Bundle())
	}
	return InviteBundle{AccountID: i.AccountID, AccountSigningPublic: append(ed25519.PublicKey(nil), i.AccountSigningPublic...), Devices: bundles}
}

func (i *Identity) Fingerprint() string {
	return Fingerprint(i.AccountSigningPublic)
}

func (c *Contact) Fingerprint() string {
	return Fingerprint(c.AccountSigningPublic)
}

func (i *Identity) CurrentDevice() (*Device, error) {
	for idx := range i.Devices {
		if i.Devices[idx].ID == i.CurrentDeviceID {
			if i.Devices[idx].Revoked {
				return nil, fmt.Errorf("current device %s is revoked", i.Devices[idx].Mailbox)
			}
			return &i.Devices[idx], nil
		}
	}
	return nil, fmt.Errorf("current device is missing")
}

func (i *Identity) CurrentMailbox() (string, error) {
	device, err := i.CurrentDevice()
	if err != nil {
		return "", err
	}
	return device.Mailbox, nil
}

func (i *Identity) EncryptionKeyPair() (*[32]byte, *[32]byte, error) {
	device, err := i.CurrentDevice()
	if err != nil {
		return nil, nil, err
	}
	return device.EncryptionKeyPair()
}

func (i *Identity) Approve(request EnrollmentRequest) (*EnrollmentApproval, error) {
	if request.AccountID != i.AccountID {
		return nil, fmt.Errorf("enrollment request account mismatch")
	}
	device := request.Device.publicOnly()
	device.ID = request.DeviceID
	device.Mailbox = request.Mailbox
	device.Signature = ed25519.Sign(i.AccountSigningPrivate, deviceStatement(i.AccountID, device.ID, device.Mailbox, device.SigningPublic, device.EncryptionPublic))
	i.upsertDevice(device)

	payload := approvalPayload{
		AccountID:             i.AccountID,
		AccountSigningPublic:  append(ed25519.PublicKey(nil), i.AccountSigningPublic...),
		AccountSigningPrivate: append(ed25519.PrivateKey(nil), i.AccountSigningPrivate...),
		Devices:               i.DeviceBundles(),
		CurrentDeviceID:       device.ID,
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode approval payload: %w", err)
	}
	recipientPublic, err := bytesToKey(device.EncryptionPublic)
	if err != nil {
		return nil, fmt.Errorf("invalid enrollment encryption key: %w", err)
	}
	ciphertext, err := box.SealAnonymous(nil, plaintext, recipientPublic, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("seal approval payload: %w", err)
	}
	return &EnrollmentApproval{AccountID: i.AccountID, Ciphertext: base64.StdEncoding.EncodeToString(ciphertext)}, nil
}

func (i *Identity) RevokeDevice(identifier string) error {
	for idx := range i.Devices {
		if i.Devices[idx].ID == identifier || i.Devices[idx].Mailbox == identifier {
			if i.Devices[idx].ID == i.CurrentDeviceID {
				return fmt.Errorf("cannot revoke the current device from itself")
			}
			i.Devices[idx].Revoked = true
			i.Devices[idx].RevokedAt = time.Now().UTC()
			return nil
		}
	}
	return fmt.Errorf("device %q not found", identifier)
}

func (i *Identity) DeviceBundles() []DeviceBundle {
	bundles := make([]DeviceBundle, 0, len(i.Devices))
	for _, device := range i.Devices {
		bundles = append(bundles, device.Bundle())
	}
	return bundles
}

func (i *Identity) Validate() error {
	if i.AccountID == "" {
		return fmt.Errorf("account id is required")
	}
	if len(i.AccountSigningPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid account signing public key")
	}
	if len(i.AccountSigningPrivate) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid account signing private key")
	}
	if _, err := i.CurrentDevice(); err != nil {
		return err
	}
	for _, device := range i.Devices {
		if err := verifyDeviceBundle(i.AccountID, i.AccountSigningPublic, device.Bundle()); err != nil {
			return err
		}
	}
	return nil
}

func ContactFromInvite(bundle InviteBundle) (*Contact, error) {
	if err := VerifyInvite(bundle); err != nil {
		return nil, err
	}
	contact := &Contact{AccountID: bundle.AccountID, AccountSigningPublic: append(ed25519.PublicKey(nil), bundle.AccountSigningPublic...), Devices: make([]ContactDevice, 0, len(bundle.Devices)), Verified: false}
	for _, device := range bundle.Devices {
		contact.Devices = append(contact.Devices, ContactDevice{ID: device.ID, Mailbox: device.Mailbox, SigningPublic: append(ed25519.PublicKey(nil), device.SigningPublic...), EncryptionPublic: append([]byte(nil), device.EncryptionPublic...), Revoked: device.Revoked, RevokedAt: device.RevokedAt})
	}
	return contact, nil
}

func VerifyInvite(bundle InviteBundle) error {
	if bundle.AccountID == "" {
		return fmt.Errorf("invite account id is required")
	}
	if len(bundle.AccountSigningPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid account signing public key")
	}
	if len(bundle.Devices) == 0 {
		return fmt.Errorf("invite must contain at least one device")
	}
	for _, device := range bundle.Devices {
		if device.Revoked {
			continue
		}
		if err := verifyDeviceBundle(bundle.AccountID, bundle.AccountSigningPublic, device); err != nil {
			return err
		}
	}
	return nil
}

func (c *Contact) ActiveDevices() []ContactDevice {
	devices := make([]ContactDevice, 0, len(c.Devices))
	for _, device := range c.Devices {
		if device.Revoked {
			continue
		}
		devices = append(devices, device)
	}
	return devices
}

func (c *Contact) DeviceByMailbox(mailbox string) (*ContactDevice, error) {
	for idx := range c.Devices {
		if c.Devices[idx].Mailbox == mailbox {
			return &c.Devices[idx], nil
		}
	}
	return nil, fmt.Errorf("device mailbox %q not found", mailbox)
}

func (d Device) Bundle() DeviceBundle {
	return DeviceBundle{ID: d.ID, Mailbox: d.Mailbox, SigningPublic: append(ed25519.PublicKey(nil), d.SigningPublic...), EncryptionPublic: append([]byte(nil), d.EncryptionPublic...), Signature: append([]byte(nil), d.Signature...), Revoked: d.Revoked, RevokedAt: d.RevokedAt}
}

func (d Device) publicOnly() Device {
	return Device{ID: d.ID, Mailbox: d.Mailbox, SigningPublic: append(ed25519.PublicKey(nil), d.SigningPublic...), EncryptionPublic: append([]byte(nil), d.EncryptionPublic...), Signature: append([]byte(nil), d.Signature...), Revoked: d.Revoked, RevokedAt: d.RevokedAt}
}

func (d Device) EncryptionKeyPair() (*[32]byte, *[32]byte, error) {
	publicKey, err := bytesToKey(d.EncryptionPublic)
	if err != nil {
		return nil, nil, err
	}
	secretKey, err := bytesToKey(d.EncryptionPrivate)
	if err != nil {
		return nil, nil, err
	}
	return publicKey, secretKey, nil
}

func newDevice(mailbox string) (Device, error) {
	deviceSignPub, deviceSignPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Device{}, fmt.Errorf("generate device signing key: %w", err)
	}
	deviceEncPub, deviceEncPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return Device{}, fmt.Errorf("generate device encryption key: %w", err)
	}
	return Device{ID: uuid.NewString(), Mailbox: mailbox, SigningPublic: deviceSignPub, SigningPrivate: deviceSignPriv, EncryptionPublic: append([]byte(nil), deviceEncPub[:]...), EncryptionPrivate: append([]byte(nil), deviceEncPriv[:]...)}, nil
}

func (i *Identity) upsertDevice(device Device) {
	for idx := range i.Devices {
		if i.Devices[idx].ID == device.ID || i.Devices[idx].Mailbox == device.Mailbox {
			i.Devices[idx].SigningPublic = append(ed25519.PublicKey(nil), device.SigningPublic...)
			i.Devices[idx].EncryptionPublic = append([]byte(nil), device.EncryptionPublic...)
			i.Devices[idx].Signature = append([]byte(nil), device.Signature...)
			i.Devices[idx].Revoked = device.Revoked
			i.Devices[idx].RevokedAt = device.RevokedAt
			return
		}
	}
	i.Devices = append(i.Devices, device)
}

func verifyDeviceBundle(accountID string, accountPublic ed25519.PublicKey, bundle DeviceBundle) error {
	if bundle.ID == "" {
		return fmt.Errorf("device id is required")
	}
	if bundle.Mailbox == "" {
		return fmt.Errorf("device mailbox is required")
	}
	if len(bundle.SigningPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid device signing public key")
	}
	if _, err := bytesToKey(bundle.EncryptionPublic); err != nil {
		return fmt.Errorf("invalid device encryption public key: %w", err)
	}
	if !ed25519.Verify(accountPublic, deviceStatement(accountID, bundle.ID, bundle.Mailbox, bundle.SigningPublic, bundle.EncryptionPublic), bundle.Signature) {
		return fmt.Errorf("device bundle signature is invalid")
	}
	return nil
}

func deviceStatement(accountID, deviceID, mailbox string, deviceSignPub ed25519.PublicKey, deviceEncPub []byte) []byte {
	statement, _ := json.Marshal(struct {
		AccountID              string `json:"account_id"`
		DeviceID               string `json:"device_id"`
		Mailbox                string `json:"mailbox"`
		DeviceSigningPublic    string `json:"device_signing_public"`
		DeviceEncryptionPublic string `json:"device_encryption_public"`
	}{
		AccountID:              accountID,
		DeviceID:               deviceID,
		Mailbox:                mailbox,
		DeviceSigningPublic:    base64.StdEncoding.EncodeToString(deviceSignPub),
		DeviceEncryptionPublic: base64.StdEncoding.EncodeToString(deviceEncPub),
	})
	return statement
}

func bytesToKey(value []byte) (*[32]byte, error) {
	if len(value) != 32 {
		return nil, fmt.Errorf("expected 32 bytes, got %d", len(value))
	}
	var key [32]byte
	copy(key[:], value)
	return &key, nil
}

func Fingerprint(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return hex.EncodeToString(hash[:8])
}
