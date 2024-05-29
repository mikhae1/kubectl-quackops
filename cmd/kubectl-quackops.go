package main

import (
	"os"

	"github.com/spf13/pflag"

	"github.com/mikhae1/kubectl-quackops/pkg/cmd"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func main() {
	flags := pflag.NewFlagSet("kubectl-quackops", pflag.ExitOnError)
	pflag.CommandLine = flags

	root := cmd.NewRootCmd(genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
