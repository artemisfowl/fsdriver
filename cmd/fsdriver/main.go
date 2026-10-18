package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	// FIXME: Enable this code later on
	//portFlag := flag.String("port", "4444", "Port for the filedriver instance")
	versionFlag := flag.Bool("version", false, "Print the version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("fsdriver version %s (commit: %s, built at: %s)\n", Version, Commit, Date)
		os.Exit(0)
	}

	log.Printf("Starting filedriver version %s instance...", Version)

	// FIXME: Add the server initialisation code here
}
