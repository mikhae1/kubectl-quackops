package main

import (
	"fmt"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
)

func main() {
	fmt.Println("Testing dual-wave animation...")

	cfg := &config.Config{
		SpinnerTimeout:   100,
		DisableAnimation: false,
	}

	_ = config.Colors

	sm := lib.GetSpinnerManager(cfg)

	msg := "Waiting for LLM response... please wait"
	fmt.Printf("\nMessage: %s\n\n", msg)

	cancel := sm.Show(lib.SpinnerLLM, msg)

	time.Sleep(15 * time.Second)

	cancel()
	fmt.Println("\nDone.")
}
