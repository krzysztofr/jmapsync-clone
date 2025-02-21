// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"codeberg.org/derat/jmapsync/jmap"
	"codeberg.org/derat/jmapsync/maildir"
	"codeberg.org/derat/jmapsync/vlog"
)

const (
	// syncOverlapDur specifies how far before the last sync start time we should go when
	// requesting messages. I'm doing this speculatively to reduce the chances of missed
	// messages if the server doesn't make delivered messages immediately available via JMAP.
	syncOverlapDur = time.Hour
	// defaultQueryChanSize is the size for the buffered channel used to return query results.
	defaultQueryChanSize = 100
	// emailAddressWidth is the default width for email addresses which listing messages.
	emailAddressWidth = 20
)

// syncConfig configures sync's behavior.
type syncConfig struct {
	dbPath        string    // path to stateDB SQLite file
	maildir       string    // path to destination Maildir directory
	minTime       time.Time // min receivedAt time (inclusive)
	maxTime       time.Time // max receivedAt time (exclusive)
	mailboxName   string    // mailbox to sync; empty for all
	startTime     time.Time // time when sync started
	list          bool      // list messages instead of downloading them
	stdout        io.Writer // write progress here instead of stdout if non-nil
	queryChanSize int       // size for query result channel, 0 for default
}

// sync uses session to sync messages per cfg.
func sync(ctx context.Context, cfg syncConfig, session session) *cmdError {
	var totalEmails uint64
	qcfg := jmap.QueryConfig{
		After:          cfg.minTime,
		Before:         cfg.maxTime,
		MailboxName:    cfg.mailboxName,
		TotalEmailsOut: &totalEmails,
		GetDetails:     cfg.list,
	}
	oldIDs := make(map[string]struct{})
	var mdir *maildir.Maildir
	var db *stateDB

	vlog.Log(ctx, "Starting sync at ", formatTime(cfg.startTime))

	stdout := cfg.stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	if !cfg.list {
		if cfg.maildir == "" {
			return cmdErrorf(2, "No destination Maildir directory specified")
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
		if qcfg.After.IsZero() {
			if qcfg.After, err = db.getLastSyncStart(); err != nil {
				return cmdErrorf(1, "Failed getting last sync time: %v", err)
			}
			vlog.Log(ctx, "Last sync started at ", formatTime(qcfg.After))
			if !qcfg.After.IsZero() {
				qcfg.After = qcfg.After.Add(-syncOverlapDur)
				vlog.Log(ctx, "Will request messages received after ", formatTime(qcfg.After))
			}
		}

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
	chanSize := cfg.queryChanSize
	if chanSize == 0 {
		chanSize = defaultQueryChanSize
	}
	emailChan := make(chan jmap.Email, chanSize)
	errChan := make(chan error, 1)
	go func() {
		if err := session.Query(ctx, qcfg, emailChan); err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Download messages as we receive IDs.
	var emailIdx int
	var countFmt string
	newIDs := make(map[string]struct{})
	for email := range emailChan {
		emailIdx++

		// Make sure we don't handle the message again if we saw it earlier due to
		// batching weirdness -- I didn't see anything in the RFCs about query cursors,
		// so I'm worried the results could change while we're fetching them.
		if setContains(newIDs, email.ID) {
			vlog.Logf(ctx, "Skipping %v (already saw during this sync)", email.ID)
			continue
		}
		newIDs[email.ID] = struct{}{}

		if cfg.list {
			var from string
			if len(email.From) > 0 {
				from = truncate(email.From[0].Email, emailAddressWidth, true /* elide */)
			}
			fmt.Fprintf(stdout, "%v  %v  %-"+strconv.Itoa(emailAddressWidth)+"s  %v\n",
				email.ID, email.ReceivedAt.Local().Format(time.DateTime), from, email.Subject)
			continue
		}

		if countFmt == "" {
			countFmt = "%" + strconv.Itoa(len(fmt.Sprint(totalEmails))) + "d"
		}

		// Don't download the message again if we already got it last time.
		if setContains(oldIDs, email.ID) {
			vlog.Logf(ctx, "Skipping %v (synced previously)", email.ID)
			fmt.Fprintf(stdout, "["+countFmt+"/"+countFmt+"] %v    (already seen)\n",
				emailIdx, totalEmails, email.ID)
			continue
		}

		vlog.Logf(ctx, "Downloading %v (blob %v)", email.ID, email.BlobID)
		r, err := session.Download(ctx, email.BlobID)
		if err != nil {
			return cmdErrorf(1, "Failed downloading message %v (blob %v): %v", email.ID, email.BlobID, err)
		}
		p, err := mdir.Deliver(r)
		r.Close()
		if err != nil {
			return cmdErrorf(1, "Failed delivering message %v: %v", email.ID, err)
		}

		fmt.Fprintf(stdout, "["+countFmt+"/"+countFmt+"] %v -> %v (%v bytes)\n",
			emailIdx, totalEmails, email.ID, filepath.Base(p), email.Size)
		vlog.Logf(ctx, "Delivered %v to %v", email.ID, p)

		// Record the ID so we don't download it again next time.
		if err := db.addLastSyncID(email.ID); err != nil {
			return cmdErrorf(1, "Failed recording synced ID: %v", err)
		}
	}
	if err := <-errChan; err != nil {
		return cmdErrorf(1, "Failed querying for messages: %v", err)
	}

	if !cfg.list {
		// Only update last-sync-related state if a max time wasn't set.
		if cfg.maxTime.IsZero() {
			vlog.Log(ctx, "Setting last sync time to ", formatTime(cfg.startTime))
			if err := db.setLastSyncStart(cfg.startTime); err != nil {
				return cmdErrorf(1, "Failed updating last sync time: %v", err)
			}

			// Clean up old synced IDs that we didn't see again this time.
			delIDs := make([]string, 0, len(oldIDs))
			for id := range oldIDs {
				if !setContains(newIDs, id) {
					delIDs = append(delIDs, id)
				}
			}
			if len(delIDs) > 0 {
				vlog.Logf(ctx, "Removing %d old synced ID(s)", len(delIDs))
				if err := db.removeLastSyncIDs(delIDs); err != nil {
					return cmdErrorf(1, "Failed cleaning old synced IDs: %v", err)
				}
			}
		} else {
			vlog.Log(ctx, "Not setting sync state since max time was set")
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
	Query(ctx context.Context, cfg jmap.QueryConfig, ch chan<- jmap.Email) error
	Download(ctx context.Context, blobID string) (io.ReadCloser, error)
}

// setContains returns true if s contains k.
func setContains(s map[string]struct{}, k string) bool {
	_, ok := s[k]
	return ok
}

// truncate truncates orig to at most max runes.
func truncate(orig string, max int, elide bool) string {
	if orig == "" || max <= 0 {
		return ""
	}
	runes := []rune(orig)
	if len(runes) <= max {
		return orig
	}
	if elide {
		return string(runes[:max-1]) + "…"
	}
	return string(runes[:max])
}

// formatTime formats t as an RFC 3339 local time.
func formatTime(t time.Time) string {
	return t.Local().Format(time.RFC3339)
}
