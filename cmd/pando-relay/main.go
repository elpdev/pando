package main

import (
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/relaycmd"
)

func main() {
	if err := relaycmd.Execute(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pando-relay error: %v\n", err)
		os.Exit(1)
	}
}
