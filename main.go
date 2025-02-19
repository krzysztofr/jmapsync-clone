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
	flag.StringVar(&cfg.mailboxName, "mailbox", "", "Name of mailbox to sync (empty to sync all messages)")
	flag.BoolVar(&cfg.list, "list", false, "List all matching messages instead of syncing them")
	flag.Var((*timeValue)(&cfg.minTime), "min-time", "Minimum received-at RFC 3339 time (empty to get all since last sync)")
	flag.Var((*timeValue)(&cfg.maxTime), "max-time", "Maximum received-at RFC 3339 time (empty to not set limit)")
	sessionURL := flag.String("session-url", "https://api.fastmail.com/jmap/session", "JMAP Session resource URL")
	flag.Parse()

	rv := func() int {
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

// timeValue implements the flag.Value interface for parsing an RFC 3339 datetime into a time.Time.
type timeValue time.Time

func (v *timeValue) String() string {
	if time.Time(*v).IsZero() {
		return ""
	}
	return time.Time(*v).Format(time.RFC3339)
}

func (v *timeValue) Set(s string) error {
	if s == "" {
		*v = timeValue(time.Time{})
		return nil
	}
	var err error
	tm, err := time.Parse(time.RFC3339, s)
	*v = timeValue(tm)
	return err
}
