package main

import (
	"os"
	"simple-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
