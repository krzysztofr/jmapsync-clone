// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"codeberg.org/derat/jmapsync/jmap"
	"codeberg.org/derat/jmapsync/maildir"
)

const (
	// syncOverlapDur specifies how far before the last sync start time we should go when
	// requesting messages. I'm doing this speculatively to reduce the chances of missed
	// messages if the server doesn't make delivered messages immediately available via JMAP.
	syncOverlapDur = time.Hour
)

func main() {
	dbPath := flag.String("db", filepath.Join(os.Getenv("HOME"), ".jmapsync.db"), "SQLite database for storing last sync state")
	mdir := flag.String("maildir", "", "Destination maildir directory (created if it doesn't exist)")
	minTime := flag.String("min-time", "", "Minimum received-at RFC 3339 time (empty to get all since last sync)")
	maxTime := flag.String("max-time", "", "Maximum received-at RFC 3339 time (empty to not set limit)")
	sessionURL := flag.String("session-url", "https://api.fastmail.com/jmap/session", "JMAP Session resource URL")
	flag.Parse()

	rv := func() int {
		if *mdir == "" {
			fmt.Fprintln(os.Stderr, "Destination maildir must be specified via -maildir")
			return 2
		}

		db, err := newStateDB(*dbPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed opening database:", err)
			return 1
		}

		var err error
		var after, before time.Time
		if *minTime != "" {
			if after, err = time.Parse(time.RFC3339, *minTime); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid -min-time %q: %v\n", *minTime, err)
				return 2
			}
		} else {
			if after, err = db.getLastSyncTime(); err != nil {
				fmt.Fprintln(os.Stderr, "Failed getting last sync time:", err)
			}
			if !after.IsZero() {
				after = after.Add(-syncOverlapDur)
			}
		}
		if *maxTime != "" {
			if before, err = time.Parse(time.RFC3339, *maxTime); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid -max-time %q: %v\n", *maxTime, err)
				return 2
			}
		}

		// Get the auth token.
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

		if err := maildir.Init(*mdir); err != nil {
			fmt.Fprintf(os.Stderr, "Failed initializing %v: %v\n", *mdir, err)
			return 1
		}

		ctx := context.Background()
		session, err := jmap.NewSession(ctx, *sessionURL, machine.password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed getting session from %v: %v\n", *sessionURL, err)
			return 1
		}

		msgs, err := session.Query(ctx, after, before)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed querying for messages:", err)
			return 1
		}

		for _, msg := range msgs {
			r, err := session.Download(ctx, msg.BlobID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed downloading message %v (blob %v): %v\n",
					msg.ID, msg.BlobID, err)
				return 1
			}
			defer r.Close()
			p, err := maildir.Deliver(*mdir, r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed delivering message %v: %v\n", msg.ID, err)
				return 1
			}
			log.Printf("%v -> %v", msg.ID, filepath.Base(p))
		}
		return 0
	}()
	os.Exit(rv)
}
