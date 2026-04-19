package relay

import (
	"errors"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
)

var queueBucket = []byte("mailboxes")
var mailboxClaimBucket = []byte("mailbox_claims")
var mailboxDirectoryBucket = []byte("mailbox_directory_accounts")
var directoryBucket = []byte("directory_entries")
var rendezvousBucket = []byte("rendezvous_entries")

var ErrQueueFull = errors.New("mailbox queue is full")
var ErrMailboxClaimConflict = errors.New("mailbox is already claimed by a different device key")
var ErrDirectoryConflict = errors.New("mailbox directory entry is already claimed by a different account key")
var ErrNotFound = errors.New("not found")

type MailboxStore interface {
	Enqueue(protocol.Envelope) error
	Drain(mailbox string) ([]protocol.Envelope, error)
	AuthorizeMailbox(mailbox string, signingPublic []byte) error
}

type DirectoryStore interface {
	PutDirectoryEntry(relayapi.SignedDirectoryEntry) error
	GetDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error)
	LookupDirectoryEntryByDeviceMailbox(mailbox string) (*relayapi.SignedDirectoryEntry, error)
	ListDiscoverableEntries() ([]relayapi.SignedDirectoryEntry, error)
	LookupMailboxAccount(mailbox string) (string, error)
}

type RendezvousStore interface {
	PutRendezvousPayload(id string, payload relayapi.RendezvousPayload) error
	GetRendezvousPayloads(id string, now time.Time) ([]relayapi.RendezvousPayload, error)
	DeleteRendezvous(id string) error
}

type QueueStore interface {
	MailboxStore
	DirectoryStore
	RendezvousStore
	SetLimits(QueueLimits)
	Close() error
}
