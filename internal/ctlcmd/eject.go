package ctlcmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/elpdev/pando/internal/config"
)

var flashDriveSearchRoots = defaultFlashDriveSearchRoots

type ejectTarget struct {
	mailbox string
	dataDir string
}

func runEject(args []string) error {
	bfs := NewBaseFlagSet("eject")
	force := bfs.FS.Bool("force", false, "skip confirmation prompt")
	flashDrive := bfs.FS.String("flash-drive", "", "mounted flash drive path to copy identities to before removal")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	targets, err := resolveEjectTargets(*bfs.RootDir, *bfs.Mailbox, *bfs.DataDir)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	resolvedFlashDrive := strings.TrimSpace(*flashDrive)
	if resolvedFlashDrive == "" && !*force {
		resolvedFlashDrive, err = promptFlashDrive(reader)
		if err != nil {
			return err
		}
	}
	if resolvedFlashDrive != "" {
		resolvedFlashDrive, err = validateFlashDrivePath(resolvedFlashDrive)
		if err != nil {
			return err
		}
	}
	if !*force {
		warning, confirmToken := ejectConfirmation(resolvedFlashDrive, *bfs.RootDir, targets)
		fmt.Fprintln(os.Stderr, warning)
		fmt.Fprintf(os.Stderr, "Type %q to confirm: ", confirmToken)
		input, readErr := reader.ReadString('\n')
		if readErr != nil {
			return fmt.Errorf("read confirmation: %w", readErr)
		}
		if strings.TrimSpace(input) != confirmToken {
			return fmt.Errorf("aborted")
		}
	}
	if resolvedFlashDrive != "" {
		for _, target := range targets {
			destination := filepath.Join(resolvedFlashDrive, "pando", "clients", target.mailbox)
			if err := copyDir(target.dataDir, destination); err != nil {
				return fmt.Errorf("copy %s to flash drive: %w", target.mailbox, err)
			}
		}
	}
	for _, target := range targets {
		if err := os.RemoveAll(target.dataDir); err != nil {
			return fmt.Errorf("eject %s: %w", target.dataDir, err)
		}
	}
	if err := clearDefaultMailbox(*bfs.RootDir, targets); err != nil {
		return err
	}
	if len(targets) == 1 {
		fmt.Printf("ejected local Pando identity %s\n", targets[0].mailbox)
		return nil
	}
	fmt.Printf("ejected %d local Pando identities\n", len(targets))
	return nil
}

func promptFlashDrive(reader *bufio.Reader) (string, error) {
	paths, err := discoverFlashDrives()
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "No mounted flash drives detected. Enter a mounted path to back up before eject, or press Enter to skip.")
		fmt.Fprintf(os.Stderr, "Flash drive path: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read flash drive path: %w", err)
		}
		return strings.TrimSpace(input), nil
	}
	fmt.Fprintln(os.Stderr, "Choose a flash drive to back up before eject, or press Enter to skip:")
	for i, path := range paths {
		fmt.Fprintf(os.Stderr, "%d. %s\n", i+1, path)
	}
	fmt.Fprintf(os.Stderr, "Flash drive [1-%d]: ", len(paths))
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read flash drive selection: %w", err)
	}
	selection := strings.TrimSpace(input)
	if selection == "" {
		return "", nil
	}
	index, err := strconv.Atoi(selection)
	if err != nil || index < 1 || index > len(paths) {
		return "", fmt.Errorf("invalid flash drive selection %q", selection)
	}
	return paths[index-1], nil
}

func discoverFlashDrives() ([]string, error) {
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	for _, root := range flashDriveSearchRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read flash drive directory %s: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func defaultFlashDriveSearchRoots() []string {
	roots := []string{"/media", "/mnt"}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		user := filepath.Base(home)
		roots = append([]string{filepath.Join("/run/media", user), filepath.Join("/media", user)}, roots...)
	}
	return roots
}

func validateFlashDrivePath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", fmt.Errorf("stat flash drive path %s: %w", cleaned, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("flash drive path %s is not a directory", cleaned)
	}
	return cleaned, nil
}

func resolveEjectTargets(rootDir, mailbox, dataDir string) ([]ejectTarget, error) {
	if mailbox == "" {
		if dataDir != "" {
			return nil, fmt.Errorf("-data-dir requires -mailbox")
		}
		return listEjectTargets(rootDir)
	}
	resolvedDataDir, err := resolveDataDir(mailbox, rootDir, dataDir)
	if err != nil {
		return nil, err
	}
	if !isIdentityDir(resolvedDataDir) {
		return nil, fmt.Errorf("identity %q not found at %s", mailbox, resolvedDataDir)
	}
	return []ejectTarget{{mailbox: mailbox, dataDir: resolvedDataDir}}, nil
}

func listEjectTargets(rootDir string) ([]ejectTarget, error) {
	clientsDir := filepath.Join(rootDir, "clients")
	entries, err := os.ReadDir(clientsDir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no local identities found under %s", clientsDir)
	}
	if err != nil {
		return nil, fmt.Errorf("read clients dir: %w", err)
	}
	targets := make([]ejectTarget, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dataDir := filepath.Join(clientsDir, entry.Name())
		if !isIdentityDir(dataDir) {
			continue
		}
		targets = append(targets, ejectTarget{mailbox: entry.Name(), dataDir: dataDir})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no local identities found under %s", clientsDir)
	}
	return targets, nil
}

func isIdentityDir(dataDir string) bool {
	info, err := os.Stat(filepath.Join(dataDir, "identity.json"))
	return err == nil && !info.IsDir()
}

func ejectConfirmation(flashDrive, rootDir string, targets []ejectTarget) (string, string) {
	if len(targets) == 1 {
		if flashDrive == "" {
			return fmt.Sprintf("This will permanently delete local Pando identity %q at %s.", targets[0].mailbox, targets[0].dataDir), targets[0].mailbox
		}
		return fmt.Sprintf("This will copy local Pando identity %q to %s and then permanently delete it from %s.", targets[0].mailbox, flashDrive, targets[0].dataDir), targets[0].mailbox
	}
	clientsDir := filepath.Join(rootDir, "clients")
	if flashDrive == "" {
		return fmt.Sprintf("This will permanently delete all local Pando identities under %s.", clientsDir), "eject all"
	}
	return fmt.Sprintf("This will copy all local Pando identities to %s and then permanently delete them from %s.", flashDrive, clientsDir), "eject all"
}

func clearDefaultMailbox(rootDir string, targets []ejectTarget) error {
	devCfg, err := config.LoadDeviceConfig(rootDir)
	if err != nil {
		return err
	}
	if devCfg.DefaultMailbox == "" {
		return nil
	}
	targetMailboxes := make([]string, 0, len(targets))
	for _, target := range targets {
		targetMailboxes = append(targetMailboxes, target.mailbox)
	}
	if !slices.Contains(targetMailboxes, devCfg.DefaultMailbox) {
		return nil
	}
	devCfg.DefaultMailbox = ""
	if err := config.SaveDeviceConfig(rootDir, devCfg); err != nil {
		return fmt.Errorf("clear default mailbox: %w", err)
	}
	return nil
}

func copyDir(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read source dir: %w", err)
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat source file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o700); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy file contents: %w", err)
	}
	return nil
}
