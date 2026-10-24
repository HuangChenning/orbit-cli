package main

import (
	"fmt"
	"github.com/HuangChenning/orbit-cli/src/internal/cli"
	"os"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "orbit:", err)
		os.Exit(1)
	}
}
