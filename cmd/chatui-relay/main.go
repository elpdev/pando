package main

import (
	"fmt"
	"os"

	"github.com/elpdev/chatui/internal/relaycmd"
)

func main() {
	if err := relaycmd.Execute(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "chatui-relay error: %v\n", err)
		os.Exit(1)
	}
}
