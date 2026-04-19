package clientcmd

import (
	"path/filepath"
	"testing"
)

func TestScanRootDirPrefersExplicitFlagValue(t *testing.T) {
	if got := scanRootDir([]string{"-mailbox", "alice", "-root-dir", "/tmp/pando-root"}); got != "/tmp/pando-root" {
		t.Fatalf("expected explicit root dir, got %q", got)
	}
	if got := scanRootDir([]string{"--root-dir=/var/lib/pando", "-mailbox", "alice"}); got != "/var/lib/pando" {
		t.Fatalf("expected inline long root dir, got %q", got)
	}
	if got := scanRootDir([]string{"-root-dir=/srv/pando", "-mailbox", "alice"}); got != "/srv/pando" {
		t.Fatalf("expected inline short root dir, got %q", got)
	}
}

func TestScanRootDirFallsBackToDefaultRoot(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	want := filepath.Join("/home/tester", ".pando")
	if got := scanRootDir([]string{"-mailbox", "alice"}); got != want {
		t.Fatalf("expected default root dir %q, got %q", want, got)
	}
}

func TestScanRootDirIgnoresMissingTrailingFlagValue(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	want := filepath.Join("/home/tester", ".pando")
	if got := scanRootDir([]string{"--root-dir"}); got != want {
		t.Fatalf("expected default root dir for missing flag value, got %q", got)
	}
}
