package main

import (
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/ctlcmd"
)

func main() {
	if err := ctlcmd.Execute(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pandoctl error: %v\n", err)
		os.Exit(1)
	}
}
