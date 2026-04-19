package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
)

type Identity struct {
	AccountID             string             `json:"account_id"`
	CurrentDeviceID       string             `json:"current_device_id"`
	AccountSigningPublic  ed25519.PublicKey  `json:"account_signing_public"`
	AccountSigningPrivate ed25519.PrivateKey `json:"account_signing_private"`
	Devices               []Device           `json:"devices"`
}

type InviteBundle struct {
	AccountID            string            `json:"account_id"`
	AccountSigningPublic ed25519.PublicKey `json:"account_signing_public"`
	Devices              []DeviceBundle    `json:"devices"`
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

func (i *Identity) InviteBundle() InviteBundle {
	bundles := make([]DeviceBundle, 0, len(i.Devices))
	for _, device := range i.Devices {
		if device.Revoked {
			continue
		}
		bundles = append(bundles, device.Bundle())
	}
	return InviteBundle{
		AccountID:            i.AccountID,
		AccountSigningPublic: clonePubKey(i.AccountSigningPublic),
		Devices:              bundles,
	}
}

func (i *Identity) Fingerprint() string {
	return Fingerprint(i.AccountSigningPublic)
}

func (i *Identity) CurrentDevice() (*Device, error) {
	device, ok := i.deviceByID(i.CurrentDeviceID)
	if !ok {
		return nil, fmt.Errorf("current device is missing")
	}
	if device.Revoked {
		return nil, fmt.Errorf("current device %s is revoked", device.Mailbox)
	}
	return device, nil
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

func (i *Identity) RevokeDevice(identifier string) error {
	device, ok := i.deviceByID(identifier)
	if !ok {
		device, ok = i.deviceByMailbox(identifier)
	}
	if !ok {
		return fmt.Errorf("device %q not found", identifier)
	}
	if device.ID == i.CurrentDeviceID {
		return fmt.Errorf("cannot revoke the current device from itself")
	}
	device.Revoked = true
	device.RevokedAt = nowUTC()
	return nil
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
