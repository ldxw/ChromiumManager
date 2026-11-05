//go:build !windows

package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

// runUI blocks until the process receives an interrupt or SIGTERM. Container
// builds have no desktop session, so the server simply runs until asked to stop.
func runUI() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	sig := <-quit
	log.Printf("[App] shutdown signal received (%v)", sig)
}
