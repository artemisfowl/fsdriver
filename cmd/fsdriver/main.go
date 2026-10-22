package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fsdriver/internal/server"
)

// Build-time version injection variables (via Makefile ldflags)
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Metadata constants
const (
	Author  = "Sayantan Bhattacharya"
	License = "GNU General Public License v3.0 (GPLv3)"
)

func main() {
	// Customize the help output to include metadata
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "fsdriver - A cross-platform, language-agnostic file monitoring driver\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Author:  %s\n", Author)
		fmt.Fprintf(flag.CommandLine.Output(), "License: %s\n\n", License)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}

	portFlag := flag.String("port", "4444", "Port for the fsdriver HTTP server")
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("fsdriver version %s (commit: %s, built at: %s)\n", Version, Commit, Date)
		fmt.Printf("License: %s\n", License)
		os.Exit(0)
	}

	// Initialize the session manager and HTTP server
	sessionManager := server.NewSessionManager()
	srv := server.NewServer(sessionManager)

	httpServer := &http.Server{
		Addr:    ":" + *portFlag,
		Handler: srv.RegisterRoutes(),
	}

	// Channel to listen for operating system termination signals
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	// Start the HTTP server in a separate background goroutine
	go func() {
		log.Printf("Starting fsdriver server on port %s...", *portFlag)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Block until a shutdown signal is received
	sig := <-shutdownChan
	log.Printf("Received shutdown signal (%v), gracefully shutting down...", sig)

	// Create a context with a 10-second timeout for active connections to finish
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server forced shutdown: %v", err)
	}

	log.Println("fsdriver stopped successfully.")
}

