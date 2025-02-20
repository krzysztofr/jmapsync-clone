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
	"codeberg.org/derat/jmapsync/vlog"
)

func main() {
	var cfg syncConfig

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %v [flag]...\n"+
			"Download email messages from a JMAP server.\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.StringVar(&cfg.dbPath, "db", filepath.Join(os.Getenv("HOME"), ".jmapsync.db"), "SQLite database for storing last sync state")
	flag.StringVar(&cfg.maildir, "maildir", "", "Destination maildir directory (created if it doesn't exist)")
	flag.StringVar(&cfg.mailboxName, "mailbox", "", "Name of mailbox to sync (empty to sync all messages)")
	flag.BoolVar(&cfg.list, "list", false, "List all matching messages instead of syncing them")
	flag.Var((*timeValue)(&cfg.minTime), "min-time", "Minimum received-at RFC 3339 time (empty to get all since last sync)")
	flag.Var((*timeValue)(&cfg.maxTime), "max-time", "Maximum received-at RFC 3339 time (empty to not set limit)")
	logPath := flag.String("log-file", "", "Path to file where verbose logs will be written")
	sessionURL := flag.String("session-url", "https://api.fastmail.com/jmap/session", "JMAP Session resource URL")
	flag.Parse()

	rv := func() int {
		if flag.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "Positional arguments unsupported")
			flag.Usage()
			return 2
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

		ctx := context.Background()
		if *logPath != "" {
			f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Failed creating log file:", err)
				return 1
			}
			defer func() {
				if err := f.Close(); err != nil {
					fmt.Fprintln(os.Stderr, "Failed closing log file:", err)
				}
			}()
			ctx = vlog.LoggerContext(ctx, log.New(f, "", log.LstdFlags|log.Lmicroseconds))
		}

		// Start the session.
		session, err := jmap.NewSession(ctx, *sessionURL, machine.password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed getting session from %v: %v\n", *sessionURL, err)
			return 1
		}

		// Sync messages.
		cfg.startTime = time.Now()
		if cerr := sync(ctx, cfg, session); cerr != nil {
			fmt.Fprintln(os.Stderr, cerr.msg)
			vlog.Logf(ctx, "Error: %v", cerr.msg)
			return cerr.code
		}
		vlog.Log(ctx, "Exiting successfully")
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
