package passphrase

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/elpdev/pando/internal/store"
	"golang.org/x/term"
)

const EnvVar = "PANDO_PASSPHRASE"

var (
	stdin                  = os.Stdin
	stderrWriter io.Writer = os.Stderr
	isTerminalFn           = term.IsTerminal
	readSecretFn           = term.ReadPassword
)

func PrepareClientStore(clientStore *store.ClientStore, mailbox string) error {
	state, err := clientStore.ProtectionState()
	if err != nil {
		return err
	}
	passphrase, err := resolve(state, mailbox)
	if err != nil {
		return err
	}
	if err := clientStore.UsePassphrase(passphrase); err != nil {
		return err
	}
	return nil
}

func resolve(state store.ProtectionState, mailbox string) ([]byte, error) {
	if value := os.Getenv(EnvVar); value != "" {
		return []byte(value), nil
	}
	if !isTerminalFn(int(stdin.Fd())) {
		return nil, fmt.Errorf("%s is required when stdin is not a terminal", EnvVar)
	}
	switch state {
	case store.ProtectionStateNew, store.ProtectionStatePlaintext:
		return promptSetup(mailbox)
	default:
		return promptUnlock(mailbox)
	}
}

func promptSetup(mailbox string) ([]byte, error) {
	passphrase, err := prompt("Set passphrase for mailbox %s: ", mailbox)
	if err != nil {
		return nil, err
	}
	if len(passphrase) == 0 {
		return nil, fmt.Errorf("passphrase is required")
	}
	confirm, err := prompt("Confirm passphrase for mailbox %s: ", mailbox)
	if err != nil {
		return nil, err
	}
	if string(passphrase) != string(confirm) {
		return nil, fmt.Errorf("passphrases do not match")
	}
	return passphrase, nil
}

func promptUnlock(mailbox string) ([]byte, error) {
	passphrase, err := prompt("Enter passphrase for mailbox %s: ", mailbox)
	if err != nil {
		return nil, err
	}
	if len(passphrase) == 0 {
		return nil, fmt.Errorf("passphrase is required")
	}
	return passphrase, nil
}

func prompt(format, mailbox string) ([]byte, error) {
	if _, err := fmt.Fprintf(stderrWriter, format, strings.TrimSpace(mailbox)); err != nil {
		return nil, err
	}
	value, err := readSecretFn(int(stdin.Fd()))
	if _, writeErr := fmt.Fprintln(stderrWriter); writeErr != nil && err == nil {
		err = writeErr
	}
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), value...), nil
}
