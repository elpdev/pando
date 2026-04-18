package main

import (
	"fmt"
	"os"

	"github.com/elpdev/chatui/internal/clientcmd"
)

func main() {
	if err := clientcmd.Execute(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "chatui error: %v\n", err)
		os.Exit(1)
	}
}
