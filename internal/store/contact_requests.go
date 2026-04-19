package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

const (
	ContactRequestDirectionIncoming = "incoming"
	ContactRequestDirectionOutgoing = "outgoing"

	ContactRequestStatusPending  = "pending"
	ContactRequestStatusAccepted = "accepted"
	ContactRequestStatusRejected = "rejected"
)

type ContactRequest struct {
	AccountID string                `json:"account_id"`
	Direction string                `json:"direction"`
	Status    string                `json:"status"`
	Note      string                `json:"note,omitempty"`
	Bundle    identity.InviteBundle `json:"bundle"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}

func (s *ClientStore) SaveContactRequest(request *ContactRequest) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	requests, err := s.loadContactRequestsMap()
	if err != nil {
		return err
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = time.Now().UTC()
	}
	if request.UpdatedAt.IsZero() {
		request.UpdatedAt = request.CreatedAt
	}
	requests[request.AccountID] = *request
	return s.writeJSON(s.contactRequestsPath(), requests, 0o600)
}

func (s *ClientStore) LoadContactRequest(accountID string) (*ContactRequest, error) {
	requests, err := s.loadContactRequestsMap()
	if err != nil {
		return nil, err
	}
	request, ok := requests[accountID]
	if !ok {
		return nil, ErrNotFound
	}
	copyRequest := request
	return &copyRequest, nil
}

func (s *ClientStore) ListContactRequests() ([]ContactRequest, error) {
	requests, err := s.loadContactRequestsMap()
	if err != nil {
		return nil, err
	}
	list := make([]ContactRequest, 0, len(requests))
	for _, request := range requests {
		list = append(list, request)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].AccountID < list[j].AccountID })
	return list, nil
}

func (s *ClientStore) DeleteContactRequest(accountID string) error {
	requests, err := s.loadContactRequestsMap()
	if err != nil {
		return err
	}
	delete(requests, accountID)
	if len(requests) == 0 {
		err := os.Remove(s.contactRequestsPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return s.writeJSON(s.contactRequestsPath(), requests, 0o600)
}

func (s *ClientStore) contactRequestsPath() string {
	return filepath.Join(s.dir, "contact-requests.json")
}

func (s *ClientStore) loadContactRequestsMap() (map[string]ContactRequest, error) {
	requests := make(map[string]ContactRequest)
	err := s.readJSON(s.contactRequestsPath(), &requests)
	if err == nil {
		return requests, nil
	}
	if errors.Is(err, ErrNotFound) {
		return requests, nil
	}
	return nil, err
}
