package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/nacl/box"
)

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

func (i *Identity) deviceByID(id string) (*Device, bool) {
	for idx := range i.Devices {
		if i.Devices[idx].ID == id {
			return &i.Devices[idx], true
		}
	}
	return nil, false
}

func (i *Identity) deviceByMailbox(mailbox string) (*Device, bool) {
	for idx := range i.Devices {
		if i.Devices[idx].Mailbox == mailbox {
			return &i.Devices[idx], true
		}
	}
	return nil, false
}

func (d Device) Bundle() DeviceBundle {
	return DeviceBundle{
		ID:               d.ID,
		Mailbox:          d.Mailbox,
		SigningPublic:    clonePubKey(d.SigningPublic),
		EncryptionPublic: cloneBytes(d.EncryptionPublic),
		Signature:        cloneBytes(d.Signature),
		Revoked:          d.Revoked,
		RevokedAt:        d.RevokedAt,
	}
}

// publicOnly strips private key material before sharing a device during enrollment.
func (d Device) publicOnly() Device {
	return Device{
		ID:               d.ID,
		Mailbox:          d.Mailbox,
		SigningPublic:    clonePubKey(d.SigningPublic),
		EncryptionPublic: cloneBytes(d.EncryptionPublic),
		Signature:        cloneBytes(d.Signature),
		Revoked:          d.Revoked,
		RevokedAt:        d.RevokedAt,
	}
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
	return Device{
		ID:                uuid.NewString(),
		Mailbox:           mailbox,
		SigningPublic:     deviceSignPub,
		SigningPrivate:    deviceSignPriv,
		EncryptionPublic:  cloneBytes(deviceEncPub[:]),
		EncryptionPrivate: cloneBytes(deviceEncPriv[:]),
	}, nil
}

func (i *Identity) upsertDevice(device Device) {
	for idx := range i.Devices {
		if i.Devices[idx].ID == device.ID || i.Devices[idx].Mailbox == device.Mailbox {
			i.Devices[idx].SigningPublic = clonePubKey(device.SigningPublic)
			i.Devices[idx].EncryptionPublic = cloneBytes(device.EncryptionPublic)
			i.Devices[idx].Signature = cloneBytes(device.Signature)
			i.Devices[idx].Revoked = device.Revoked
			i.Devices[idx].RevokedAt = device.RevokedAt
			return
		}
	}
	i.Devices = append(i.Devices, device)
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
