// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// stateDB holds state about the previous sync.
type stateDB struct{ db *sql.DB }

// newStateDB creates a new SQLite database at path.
func newStateDB(path string) (*stateDB, error) {
	db, err := sql.Open("sqlite", path+"?_locking=EXCLUSIVE&_synchronous=FULL")
	if err != nil {
		return nil, err
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()

	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS LastSyncStart (Time INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS LastSyncIDs (ID TEXT PRIMARY KEY NOT NULL)`,
	} {
		if _, err = db.Exec(q); err != nil {
			return nil, err
		}
	}

	sdb := &stateDB{db}
	db = nil // disarm Close() call
	return sdb, nil
}

func (sdb *stateDB) close() error { return sdb.db.Close() }

// getLastSyncStart returns the time previously set via setLastSyncStart.
// If no time has been set, a zero time.Time is returned.
func (sdb *stateDB) getLastSyncStart() (time.Time, error) {
	var sec int64
	if err := sdb.db.QueryRow("SELECT Time FROM LastSyncStart").Scan(&sec); errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	} else if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

// setLastSyncStart updates the last-sync timestamp in the database.
func (sdb *stateDB) setLastSyncStart(t time.Time) error {
	tx, err := sdb.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	if res, err := tx.Exec("UPDATE LastSyncStart SET Time = ?", t.Unix()); err != nil {
		return err
	} else if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n > 1 {
		return fmt.Errorf("updated %v rows; want 0 or 1", n)
	} else if n == 1 {
		// Updated the existing row.
	} else if _, err := tx.Exec("INSERT INTO LastSyncStart VALUES(?)", t.Unix()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil // defuse tx.Rollback()
	return nil
}

// getLastSyncIDs returns IDs that were previously set by addLastSyncID.
func (sdb *stateDB) getLastSyncIDs() ([]string, error) {
	rows, err := sdb.db.Query("SELECT ID FROM LastSyncIDs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// addLastSyncID adds an ID to be returned by getLastSyncID.
func (sdb *stateDB) addLastSyncID(id string) error {
	_, err := sdb.db.Exec("REPLACE INTO LastSyncIDs VALUES(?)", id)
	return err

}

// removeLastSyncIDs removes the specified IDs previously added by addLastSyncID.
func (sdb *stateDB) removeLastSyncIDs(ids []string) error {
	tx, err := sdb.db.Begin()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec("DELETE FROM LastSyncIDs WHERE ID = ?", id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// beginBatch starts a transaction for batch operations.
func (sdb *stateDB) beginBatch() (*sql.Tx, error) {
	return sdb.db.Begin()
}

// commitBatch commits a batch transaction.
func (sdb *stateDB) commitBatch(tx *sql.Tx) error {
	return tx.Commit()
}

// addLastSyncIDBatch adds an ID within a transaction (for batched operations).
func (sdb *stateDB) addLastSyncIDBatch(tx *sql.Tx, id string) error {
	_, err := tx.Exec("REPLACE INTO LastSyncIDs VALUES(?)", id)
	return err
}
