package ctlcmd

import (
	"github.com/elpdev/pando/internal/passphrase"
	"github.com/elpdev/pando/internal/store"
)

func prepareClientStore(mailbox, dataDir string) (*store.ClientStore, error) {
	clientStore := store.NewClientStore(dataDir)
	if err := passphrase.PrepareClientStore(clientStore, mailbox); err != nil {
		return nil, err
	}
	return clientStore, nil
}
