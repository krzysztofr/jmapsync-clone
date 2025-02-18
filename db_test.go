// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestStateDB_LastSyncStart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.db")
	db, err := newStateDB(p)
	if err != nil {
		t.Fatal("Creating DB failed:", err)
	}
	if tm, err := db.getLastSyncStart(); err != nil {
		t.Fatal("Getting unset value failed:", err)
	} else if !tm.IsZero() {
		t.Fatalf("Unset value is %v; want zero", tm)
	}

	t1 := time.Date(2025, 2, 1, 3, 4, 5, 0, time.UTC)
	if err := db.setLastSyncStart(t1); err != nil {
		t.Fatal("Setting initial value failed:", err)
	}
	if tm, err := db.getLastSyncStart(); err != nil {
		t.Fatal("Getting initial value failed:", err)
	} else if !tm.Equal(t1) {
		t.Fatalf("Initial value is %v; want %v", tm, t1)
	}
	if err := db.close(); err != nil {
		t.Fatal(err)
	}

	if db, err = newStateDB(p); err != nil {
		t.Fatal("Reopening DB failed:", err)
	}
	if tm, err := db.getLastSyncStart(); err != nil {
		t.Fatal("Getting value after reopening failed:", err)
	} else if !tm.Equal(t1) {
		t.Fatalf("Value after reopening is %v; want %v", tm, t1)
	}
	t2 := time.Date(2025, 3, 5, 1, 2, 3, 0, time.UTC)
	if err := db.setLastSyncStart(t2); err != nil {
		t.Fatal("Updating value failed:", err)
	}
	if tm, err := db.getLastSyncStart(); err != nil {
		t.Fatal("Getting updated value failed:", err)
	} else if !tm.Equal(t2) {
		t.Fatalf("Updated value is %v; want %v", tm, t1)
	}
	if err := db.close(); err != nil {
		t.Fatal(err)
	}
}

func TestStateDB_LastSyncIDs(t *testing.T) {
	sort := func(ids []string) []string {
		sort.Strings(ids)
		return ids
	}
	p := filepath.Join(t.TempDir(), "test.db")
	db, err := newStateDB(p)
	if err != nil {
		t.Fatal("Creating DB failed:", err)
	}

	if ids, err := db.getLastSyncIDs(); err != nil {
		t.Fatal("Getting unset IDs failed:", err)
	} else if got := sort(ids); !reflect.DeepEqual(got, []string(nil)) {
		t.Fatalf("Unset IDs are %v; want nil", got)
	}

	for _, id := range []string{"123", "456", "789"} {
		if err := db.addLastSyncID(id); err != nil {
			t.Fatalf("addLastSyncID(%q) failed: %v", id, err)
		}
	}
	if ids, err := db.getLastSyncIDs(); err != nil {
		t.Fatal("Getting initial IDs failed:", err)
	} else if got, want := sort(ids), []string{"123", "456", "789"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Initial IDs are %v; want %v", got, want)
	}

	if err := db.removeLastSyncIDs([]string{"123", "abc"}); err != nil {
		t.Fatalf("removeLastSyncIDs failed: %v", err)
	}
	if err := db.addLastSyncID("456"); err != nil {
		t.Fatalf("addLastSyncID(%q) failed: %v", "456", err)
	}
	if ids, err := db.getLastSyncIDs(); err != nil {
		t.Fatal("Getting updated IDs failed:", err)
	} else if got, want := sort(ids), []string{"456", "789"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Updated IDs are %v; want %v", got, want)
	}

	if err := db.close(); err != nil {
		t.Fatal(err)
	}
}
