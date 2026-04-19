package identity

import (
	"crypto/ed25519"
	"fmt"
	"time"
)

type Contact struct {
	AccountID            string            `json:"account_id"`
	AccountSigningPublic ed25519.PublicKey `json:"account_signing_public"`
	Devices              []ContactDevice   `json:"devices"`
	Verified             bool              `json:"verified"`
	TrustSource          string            `json:"trust_source,omitempty"`
}

type ContactDevice struct {
	ID               string            `json:"id"`
	Mailbox          string            `json:"mailbox"`
	SigningPublic    ed25519.PublicKey `json:"signing_public"`
	EncryptionPublic []byte            `json:"encryption_public"`
	Revoked          bool              `json:"revoked"`
	RevokedAt        time.Time         `json:"revoked_at,omitempty"`
}

func ContactFromInvite(bundle InviteBundle) (*Contact, error) {
	if err := VerifyInvite(bundle); err != nil {
		return nil, err
	}
	contact := &Contact{
		AccountID:            bundle.AccountID,
		AccountSigningPublic: clonePubKey(bundle.AccountSigningPublic),
		Devices:              make([]ContactDevice, 0, len(bundle.Devices)),
		Verified:             false,
		TrustSource:          TrustSourceUnverified,
	}
	for _, device := range bundle.Devices {
		contact.Devices = append(contact.Devices, ContactDevice{
			ID:               device.ID,
			Mailbox:          device.Mailbox,
			SigningPublic:    clonePubKey(device.SigningPublic),
			EncryptionPublic: cloneBytes(device.EncryptionPublic),
			Revoked:          device.Revoked,
			RevokedAt:        device.RevokedAt,
		})
	}
	return contact, nil
}

func (c *Contact) Fingerprint() string {
	return Fingerprint(c.AccountSigningPublic)
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
