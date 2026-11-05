package main

import (
	"os"

	"github.com/WellWells/line-cli/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
