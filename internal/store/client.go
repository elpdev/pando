package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/elpdev/pando/internal/identity"
)

var ErrNotFound = errors.New("not found")

type ClientStore struct {
	dir string
}

func NewClientStore(dir string) *ClientStore {
	return &ClientStore{dir: dir}
}

func (s *ClientStore) Ensure() error {
	return os.MkdirAll(s.dir, 0o700)
}

func (s *ClientStore) LoadIdentity() (*identity.Identity, error) {
	var id identity.Identity
	if err := s.readJSON(s.identityPath(), &id); err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *ClientStore) LoadOrCreateIdentity(mailbox string) (*identity.Identity, bool, error) {
	id, err := s.LoadIdentity()
	if err == nil {
		if currentMailbox, currentErr := id.CurrentMailbox(); currentErr == nil && currentMailbox != mailbox {
			return nil, false, fmt.Errorf("store belongs to device mailbox %q, not %q", currentMailbox, mailbox)
		}
		return id, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	id, err = identity.New(mailbox)
	if err != nil {
		return nil, false, err
	}
	if err := s.SaveIdentity(id); err != nil {
		return nil, false, err
	}
	return id, true, nil
}

func (s *ClientStore) SaveIdentity(id *identity.Identity) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	return s.writeJSON(s.identityPath(), id, 0o600)
}

func (s *ClientStore) SaveContact(contact *identity.Contact) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	contacts, err := s.loadContactsMap()
	if err != nil {
		return err
	}
	contacts[contact.AccountID] = *contact
	return s.writeJSON(s.contactsPath(), contacts, 0o600)
}

func (s *ClientStore) LoadContact(mailbox string) (*identity.Contact, error) {
	contacts, err := s.loadContactsMap()
	if err != nil {
		return nil, err
	}
	contact, ok := contacts[mailbox]
	if !ok {
		return nil, ErrNotFound
	}
	copyContact := contact
	return &copyContact, nil
}

func (s *ClientStore) ListContacts() ([]identity.Contact, error) {
	contacts, err := s.loadContactsMap()
	if err != nil {
		return nil, err
	}
	list := make([]identity.Contact, 0, len(contacts))
	for _, contact := range contacts {
		list = append(list, contact)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].AccountID < list[j].AccountID })
	return list, nil
}

func (s *ClientStore) MarkContactVerified(mailbox string, verified bool) (*identity.Contact, error) {
	contacts, err := s.loadContactsMap()
	if err != nil {
		return nil, err
	}
	contact, ok := contacts[mailbox]
	if !ok {
		return nil, ErrNotFound
	}
	contact.Verified = verified
	contacts[mailbox] = contact
	if err := s.writeJSON(s.contactsPath(), contacts, 0o600); err != nil {
		return nil, err
	}
	copyContact := contact
	return &copyContact, nil
}

func (s *ClientStore) LoadContactByDeviceMailbox(mailbox string) (*identity.Contact, error) {
	contacts, err := s.loadContactsMap()
	if err != nil {
		return nil, err
	}
	for _, contact := range contacts {
		if _, deviceErr := contact.DeviceByMailbox(mailbox); deviceErr == nil {
			copyContact := contact
			return &copyContact, nil
		}
	}
	return nil, ErrNotFound
}

func (s *ClientStore) SavePendingEnrollment(pending *identity.PendingEnrollment) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	return s.writeJSON(s.pendingEnrollmentPath(), pending, 0o600)
}

func (s *ClientStore) LoadPendingEnrollment() (*identity.PendingEnrollment, error) {
	var pending identity.PendingEnrollment
	if err := s.readJSON(s.pendingEnrollmentPath(), &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

func (s *ClientStore) ClearPendingEnrollment() error {
	err := os.Remove(s.pendingEnrollmentPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove %s: %w", s.pendingEnrollmentPath(), err)
	}
	return nil
}

func (s *ClientStore) identityPath() string {
	return filepath.Join(s.dir, "identity.json")
}

func (s *ClientStore) contactsPath() string {
	return filepath.Join(s.dir, "contacts.json")
}

func (s *ClientStore) pendingEnrollmentPath() string {
	return filepath.Join(s.dir, "pending-enrollment.json")
}

func (s *ClientStore) loadContactsMap() (map[string]identity.Contact, error) {
	contacts := make(map[string]identity.Contact)
	err := s.readJSON(s.contactsPath(), &contacts)
	if err == nil {
		return contacts, nil
	}
	if errors.Is(err, ErrNotFound) {
		return contacts, nil
	}
	return nil, err
}

func (s *ClientStore) readJSON(path string, target any) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(bytes, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (s *ClientStore) writeJSON(path string, value any, mode os.FileMode) error {
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, bytes, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
