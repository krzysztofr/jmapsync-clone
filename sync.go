// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"context"
	"fmt"
	"io"
	"log"
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
	// queryChanSize is the size for the buffered channel used to return query results.
	queryChanSize = 100
)

// syncConfig configures sync's behavior.
type syncConfig struct {
	dbPath    string    // path to stateDB SQLite file
	maildir   string    // path to destination maildir
	minTime   time.Time // min receivedAt time
	maxTime   time.Time // max receivedAt type
	startTime time.Time // time when sync started
}

// sync uses session to sync messages per cfg.
func sync(ctx context.Context, cfg syncConfig, session session) *cmdError {
	db, err := newStateDB(cfg.dbPath)
	if err != nil {
		return cmdErrorf(1, "Failed opening database: %v", err)
	}
	defer func() {
		if db != nil {
			db.close()
		}
	}()

	// Sync messages received since slightly before the last sync if the user didn't specify a
	// minimum time.
	after, before := cfg.minTime, cfg.maxTime
	if after.IsZero() {
		if after, err = db.getLastSyncStart(); err != nil {
			return cmdErrorf(1, "Failed getting last sync time: %v", err)
		}
		if !after.IsZero() {
			after = after.Add(-syncOverlapDur)
		}
	}

	// TODO: Skip some or all of this last-sync stuff when minTime and/or maxTime were passed?
	oldIDs := make(map[string]struct{})
	if ids, err := db.getLastSyncIDs(); err != nil {
		return cmdErrorf(1, "Failed getting last sync IDs: %v", err)
	} else {
		for _, id := range ids {
			oldIDs[id] = struct{}{}
		}
	}

	mdir, err := maildir.New(cfg.maildir)
	if err != nil {
		return cmdErrorf(1, "Failed initializing %v: %v", cfg.maildir, err)
	}

	// Start querying for messages asynchronously.
	msgChan := make(chan jmap.MessageInfo, queryChanSize)
	errChan := make(chan error, 1)
	go func() {
		if err := session.Query(ctx, after, before, msgChan); err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Download messages as we receive IDs.
	newIDs := make(map[string]struct{})
	for msg := range msgChan {
		// If the query failed, don't bother downloading messages.
		if len(errChan) != 0 {
			break
		}

		newIDs[msg.ID] = struct{}{}

		// Don't download the message again if we already got it last time.
		if _, ok := oldIDs[msg.ID]; !ok {
			continue
		}

		r, err := session.Download(ctx, msg.BlobID)
		if err != nil {
			return cmdErrorf(1, "Failed downloading message %v (blob %v): %v", msg.ID, msg.BlobID, err)
		}
		p, err := mdir.Deliver(r)
		r.Close()
		if err != nil {
			return cmdErrorf(1, "Failed delivering message %v: %v", msg.ID, err)
		}

		// Record the ID so we don't download it again next time.
		if err := db.addLastSyncID(msg.ID); err != nil {
			return cmdErrorf(1, "Failed recording synced ID: %v", err)
		}

		log.Printf("%v -> %v", msg.ID, filepath.Base(p))
	}
	if err := <-errChan; err != nil {
		return cmdErrorf(1, "Failed querying for messages: %v", err)
	}

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

	err = db.close()
	db = nil // disarm db.close() call
	if err != nil {
		return cmdErrorf(1, "Failed closing database: %v", err)
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
	Query(ctx context.Context, after, before time.Time, ch chan<- jmap.MessageInfo) error
	Download(ctx context.Context, blobID string) (io.ReadCloser, error)
}
