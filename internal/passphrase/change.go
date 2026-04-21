package passphrase

import (
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/store"
)

const NewEnvVar = "PANDO_PASSPHRASE_NEW"

func ChangeClientStorePassphrase(clientStore *store.ClientStore, mailbox string) error {
	passphrase, err := resolveNew(mailbox)
	if err != nil {
		return err
	}
	if err := clientStore.ChangePassphrase(passphrase); err != nil {
		return err
	}
	return nil
}

func resolveNew(mailbox string) ([]byte, error) {
	if value := os.Getenv(NewEnvVar); value != "" {
		return []byte(value), nil
	}
	if !isTerminalFn(int(stdin.Fd())) {
		return nil, fmt.Errorf("%s is required when stdin is not a terminal", NewEnvVar)
	}
	return promptSetup(mailbox)
}
