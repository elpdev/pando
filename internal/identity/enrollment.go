package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

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
		id.Devices = append(id.Devices, deviceFromBundle(bundle, bundle.ID == p.Device.ID, &p.Device))
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return id, nil
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
		AccountSigningPublic:  clonePubKey(i.AccountSigningPublic),
		AccountSigningPrivate: clonePrivKey(i.AccountSigningPrivate),
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

func deviceFromBundle(bundle DeviceBundle, withPrivateKeys bool, pending *Device) Device {
	device := Device{
		ID:               bundle.ID,
		Mailbox:          bundle.Mailbox,
		SigningPublic:    clonePubKey(bundle.SigningPublic),
		EncryptionPublic: cloneBytes(bundle.EncryptionPublic),
		Signature:        cloneBytes(bundle.Signature),
		Revoked:          bundle.Revoked,
		RevokedAt:        bundle.RevokedAt,
	}
	if withPrivateKeys && pending != nil {
		device.SigningPrivate = clonePrivKey(pending.SigningPrivate)
		device.EncryptionPrivate = cloneBytes(pending.EncryptionPrivate)
	}
	return device
}
