package passphrase

import (
	"bytes"
	"os"
	"testing"

	"github.com/elpdev/pando/internal/store"
)

func TestResolveUsesEnvironmentPassphrase(t *testing.T) {
	t.Setenv(EnvVar, "env-secret")
	passphrase, err := resolve(store.ProtectionStateEncrypted, "alice")
	if err != nil {
		t.Fatalf("resolve passphrase from env: %v", err)
	}
	if string(passphrase) != "env-secret" {
		t.Fatalf("unexpected env passphrase %q", string(passphrase))
	}
}

func TestResolveFailsWithoutTTYOrEnvironment(t *testing.T) {
	originalIsTerminal := isTerminalFn
	defer func() { isTerminalFn = originalIsTerminal }()
	isTerminalFn = func(int) bool { return false }
	t.Setenv(EnvVar, "")
	_, err := resolve(store.ProtectionStateEncrypted, "alice")
	if err == nil {
		t.Fatal("expected resolve to fail without tty or env passphrase")
	}
}

func TestResolvePromptsForNewStore(t *testing.T) {
	originalStdin := stdin
	originalStderr := stderrWriter
	originalIsTerminal := isTerminalFn
	originalReadSecret := readSecretFn
	defer func() {
		stdin = originalStdin
		stderrWriter = originalStderr
		isTerminalFn = originalIsTerminal
		readSecretFn = originalReadSecret
	}()
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer file.Close()
	stdin = file
	var output bytes.Buffer
	stderrWriter = &output
	isTerminalFn = func(int) bool { return true }
	responses := [][]byte{[]byte("secret-passphrase"), []byte("secret-passphrase")}
	readSecretFn = func(int) ([]byte, error) {
		response := responses[0]
		responses = responses[1:]
		return response, nil
	}
	passphrase, err := resolve(store.ProtectionStateNew, "alice")
	if err != nil {
		t.Fatalf("resolve setup passphrase: %v", err)
	}
	if string(passphrase) != "secret-passphrase" {
		t.Fatalf("unexpected setup passphrase %q", string(passphrase))
	}
	if output.Len() == 0 {
		t.Fatal("expected prompt output for setup passphrase")
	}
}

func TestResolveNewUsesEnvironmentPassphrase(t *testing.T) {
	t.Setenv(NewEnvVar, "new-env-secret")
	passphrase, err := resolveNew("alice")
	if err != nil {
		t.Fatalf("resolve new passphrase from env: %v", err)
	}
	if string(passphrase) != "new-env-secret" {
		t.Fatalf("unexpected new env passphrase %q", string(passphrase))
	}
}
