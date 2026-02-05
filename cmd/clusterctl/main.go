package main

import (
	"fmt"
	"os"

	"github.com/dave/clusterctl/internal/cli"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	rootCmd := cli.NewRootCmd(Version, BuildTime)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
