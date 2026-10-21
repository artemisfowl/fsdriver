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

// Metadata constants
const (
    Author = "artemisfowl <skb655952@gmail.com>"
    License = "GNU General Public License v3.0 (GPLv3)"
)

func main() {
    flag.Usage = func() {
        fmt.Fprintf(
            flag.CommandLine.Output(),
            "fsdriver - A Cross-platform, language agnostic file monitoring driver\n\n",
        )
        fmt.Fprintf(flag.CommandLine.Output(), "Author: %s\n", Author)
        fmt.Fprintf(flag.CommandLine.Output(), "License: %s\n\n", License)
        fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s\n", os.Args[0])
        flag.PrintDefaults()
    }
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
