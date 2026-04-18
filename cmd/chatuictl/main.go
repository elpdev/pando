package main

import (
	"fmt"
	"os"

	"github.com/elpdev/chatui/internal/ctlcmd"
)

func main() {
	if err := ctlcmd.Execute(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "chatuictl error: %v\n", err)
		os.Exit(1)
	}
}
