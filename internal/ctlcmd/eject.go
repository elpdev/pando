package ctlcmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func runEject(args []string) error {
	bfs := NewBaseFlagSet("eject")
	force := bfs.FS.Bool("force", false, "skip confirmation prompt")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if !*force {
		fmt.Fprintf(os.Stderr, "This will permanently delete all local Pando data for mailbox %q at %s.\n", mailbox, resolvedDataDir)
		fmt.Fprintf(os.Stderr, "Type the mailbox name to confirm: ")
		reader := bufio.NewReader(os.Stdin)
		input, readErr := reader.ReadString('\n')
		if readErr != nil {
			return fmt.Errorf("read confirmation: %w", readErr)
		}
		if strings.TrimSpace(input) != mailbox {
			return fmt.Errorf("aborted")
		}
	}
	if err := os.RemoveAll(resolvedDataDir); err != nil {
		return fmt.Errorf("eject %s: %w", resolvedDataDir, err)
	}
	fmt.Printf("ejected local Pando data for %s\n", mailbox)
	return nil
}
