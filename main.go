// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"codeberg.org/derat/jmapsync/jmap"
)

func main() {
	var cfg syncConfig
	flag.StringVar(&cfg.dbPath, "db", filepath.Join(os.Getenv("HOME"), ".jmapsync.db"), "SQLite database for storing last sync state")
	flag.StringVar(&cfg.maildir, "maildir", "", "Destination maildir directory (created if it doesn't exist)")
	minTime := flag.String("min-time", "", "Minimum received-at RFC 3339 time (empty to get all since last sync)")
	maxTime := flag.String("max-time", "", "Maximum received-at RFC 3339 time (empty to not set limit)")
	sessionURL := flag.String("session-url", "https://api.fastmail.com/jmap/session", "JMAP Session resource URL")
	flag.Parse()

	rv := func() int {
		// Validate flags.
		if cfg.maildir == "" {
			fmt.Fprintln(os.Stderr, "Destination maildir must be specified via -maildir")
			return 2
		}
		if *minTime != "" {
			var err error
			if cfg.minTime, err = time.Parse(time.RFC3339, *minTime); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid -min-time %q: %v\n", *minTime, err)
				return 2
			}
		}
		if *maxTime != "" {
			var err error
			if cfg.maxTime, err = time.Parse(time.RFC3339, *maxTime); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid -max-time %q: %v\n", *maxTime, err)
				return 2
			}
		}

		// Read the auth token from .netrc.
		u, err := url.Parse(*sessionURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad JMAP Session resource URL %q: %v\n", *sessionURL, err)
			return 2
		}
		netrcPath := filepath.Join(os.Getenv("HOME"), ".netrc")
		machine, err := readNetrcMachine(netrcPath, u.Hostname())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed reading %v: %v\n", netrcPath, err)
			return 2
		} else if machine == nil {
			fmt.Fprintf(os.Stderr, "Didn't find machine %q in %v\n", u.Hostname(), netrcPath)
			return 2
		}

		// Start the session.
		ctx := context.Background()
		session, err := jmap.NewSession(ctx, *sessionURL, machine.password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed getting session from %v: %v\n", *sessionURL, err)
			return 1
		}

		// Sync messages.
		cfg.startTime = time.Now()
		if cerr := sync(ctx, cfg, session); cerr != nil {
			fmt.Fprintln(os.Stderr, cerr.msg)
			return cerr.code
		}
		return 0
	}()
	os.Exit(rv)
}
