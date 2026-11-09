// Command sil is the esphome-sil simulator CLI.
package main

import (
	"os"

	"github.com/zeroflow/esphome-sil/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
