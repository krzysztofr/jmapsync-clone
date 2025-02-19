// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"codeberg.org/derat/jmapsync/jmap"
	"codeberg.org/derat/jmapsync/maildir"
)

const (
	// syncOverlapDur specifies how far before the last sync start time we should go when
	// requesting messages. I'm doing this speculatively to reduce the chances of missed
	// messages if the server doesn't make delivered messages immediately available via JMAP.
	syncOverlapDur = time.Hour
	// queryChanSize is the size for the buffered channel used to return query results.
	queryChanSize = 100
	// emailAddressWidth is the default width for email addresses which listing messages.
	emailAddressWidth = 20
)

// syncConfig configures sync's behavior.
type syncConfig struct {
	dbPath      string    // path to stateDB SQLite file
	maildir     string    // path to destination maildir
	minTime     time.Time // min receivedAt time
	maxTime     time.Time // max receivedAt type
	mailboxName string    // mailbox to sync; empty for all
	startTime   time.Time // time when sync started
	list        bool      // list messages instead of downloading them
}

// sync uses session to sync messages per cfg.
func sync(ctx context.Context, cfg syncConfig, session session) *cmdError {
	filter := jmap.QueryFilter{
		After:       cfg.minTime,
		Before:      cfg.maxTime,
		MailboxName: cfg.mailboxName,
	}
	oldIDs := make(map[string]struct{})
	var mdir *maildir.Maildir
	var db *stateDB

	if !cfg.list {
		if cfg.maildir == "" {
			return cmdErrorf(2, "No destination maildir specified")
		}

		var err error
		if db, err = newStateDB(cfg.dbPath); err != nil {
			return cmdErrorf(1, "Failed opening database: %v", err)
		}
		defer func() {
			if db != nil {
				db.close()
			}
		}()

		// Sync messages received since slightly before the last sync if the user didn't specify a
		// minimum time.
		if filter.After.IsZero() {
			if filter.After, err = db.getLastSyncStart(); err != nil {
				return cmdErrorf(1, "Failed getting last sync time: %v", err)
			}
			if !filter.After.IsZero() {
				filter.After = filter.After.Add(-syncOverlapDur)
			}
		}

		// TODO: Skip some or all of this last-sync stuff when minTime and/or maxTime were passed?
		if ids, err := db.getLastSyncIDs(); err != nil {
			return cmdErrorf(1, "Failed getting last sync IDs: %v", err)
		} else {
			for _, id := range ids {
				oldIDs[id] = struct{}{}
			}
		}

		if mdir, err = maildir.New(cfg.maildir); err != nil {
			return cmdErrorf(1, "Failed initializing %v: %v", cfg.maildir, err)
		}
	}

	// Start querying for messages asynchronously.
	emailChan := make(chan jmap.Email, queryChanSize)
	errChan := make(chan error, 1)
	go func() {
		if err := session.Query(ctx, filter, emailChan); err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Download messages as we receive IDs.
	newIDs := make(map[string]struct{})
	for email := range emailChan {
		// If the query failed, don't bother downloading messages.
		if len(errChan) != 0 {
			break
		}

		if cfg.list {
			var from string
			if len(email.From) > 0 {
				from = truncate(email.From[0].Email, emailAddressWidth, true /* elide */)
			}
			fmt.Printf("%v  %-"+strconv.Itoa(emailAddressWidth)+"s  %v\n", email.ID, from, email.Subject)
		} else {
			newIDs[email.ID] = struct{}{}

			// Don't download the message again if we already got it last time.
			if _, ok := oldIDs[email.ID]; !ok {
				continue
			}

			r, err := session.Download(ctx, email.BlobID)
			if err != nil {
				return cmdErrorf(1, "Failed downloading message %v (blob %v): %v", email.ID, email.BlobID, err)
			}
			p, err := mdir.Deliver(r)
			r.Close()
			if err != nil {
				return cmdErrorf(1, "Failed delivering message %v: %v", email.ID, err)
			}

			// Record the ID so we don't download it again next time.
			if err := db.addLastSyncID(email.ID); err != nil {
				return cmdErrorf(1, "Failed recording synced ID: %v", err)
			}

			fmt.Printf("%v -> %v\n", email.ID, filepath.Base(p))
		}
	}
	if err := <-errChan; err != nil {
		return cmdErrorf(1, "Failed querying for messages: %v", err)
	}

	if !cfg.list {
		if err := db.setLastSyncStart(cfg.startTime); err != nil {
			return cmdErrorf(1, "Failed updating last sync time: %v", err)
		}

		// Clean up old synced IDs that we didn't see again this time.
		delIDs := make([]string, 0, len(oldIDs))
		for id := range oldIDs {
			if _, ok := newIDs[id]; !ok {
				delIDs = append(delIDs, id)
			}
		}
		if err := db.removeLastSyncIDs(delIDs); err != nil {
			return cmdErrorf(1, "Failed cleaning old synced IDs: %v", err)
		}

		err := db.close()
		db = nil // disarm db.close() call
		if err != nil {
			return cmdErrorf(1, "Failed closing database: %v", err)
		}
	}
	return nil
}

// cmdError contains an exit code and error message to print for the user.
type cmdError struct {
	code int
	msg  string
}

func cmdErrorf(code int, format string, args ...any) *cmdError {
	return &cmdError{code, fmt.Sprintf(format, args...)}
}

// session wraps jmap.Session for testing.
type session interface {
	Query(ctx context.Context, filter jmap.QueryFilter, ch chan<- jmap.Email) error
	Download(ctx context.Context, blobID string) (io.ReadCloser, error)
}

func truncate(s string, max int, elide bool) string {
	if len(s) <= max {
		return s
	}
	if elide {
		return s[:max-1] + "…"
	}
	return s[:max]
}
