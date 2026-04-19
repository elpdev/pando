package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/elpdev/pando/internal/clientcmd"
	"github.com/elpdev/pando/internal/ctlcmd"
)

func main() {
	args := os.Args[1:]
	var err error
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		err = clientcmd.Execute(args)
	} else if ctlcmd.IsSubcommand(args[0]) {
		err = ctlcmd.Execute(args)
	} else {
		err = fmt.Errorf("unknown subcommand %q", args[0])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pando error: %v\n", err)
		os.Exit(1)
	}
}
