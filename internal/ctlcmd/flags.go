package ctlcmd

import (
	"flag"
	"os"

	"github.com/elpdev/pando/internal/config"
)

type BaseFlagSet struct {
	Name    string
	Mailbox *string
	RootDir *string
	DataDir *string
	FS      *flag.FlagSet
}

func NewBaseFlagSet(name string) *BaseFlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	return &BaseFlagSet{
		Name:    name,
		Mailbox: mailbox,
		RootDir: rootDir,
		DataDir: dataDir,
		FS:      fs,
	}
}

func (b *BaseFlagSet) Parse(args []string) error {
	return b.FS.Parse(args)
}

func (b *BaseFlagSet) Resolve() (string, string, error) {
	devCfg, err := config.LoadDeviceConfig(*b.RootDir)
	if err != nil {
		return "", "", err
	}
	resolvedMailbox := *b.Mailbox
	if resolvedMailbox == "" {
		resolvedMailbox = devCfg.DefaultMailbox
	}
	resolvedDataDir, err := resolveDataDir(resolvedMailbox, *b.RootDir, *b.DataDir)
	if err != nil {
		return "", "", err
	}
	return resolvedMailbox, resolvedDataDir, nil
}
