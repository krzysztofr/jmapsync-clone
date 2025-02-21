// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"codeberg.org/derat/jmapsync/jmap"
)

func TestSync_Basic(t *testing.T) {
	cfg, _ := makeTestConfig(t)
	session := newTestSession()

	// Add 10 messages and sync them.
	emails := session.addEmails(1, 10, date("2024-11-02 14:15:00"), time.Minute)
	doTestSync(t, cfg, session, date("2024-11-02 15:00:00"), true)
	verifyMaildir(t, cfg.maildir, emails)

	// Doing another sync a minute later should be a no-op.
	doTestSync(t, cfg, session, date("2024-11-02 15:01:00"), true)
	verifyMaildir(t, cfg.maildir, emails)

	// Add 5 more messages and sync them.
	emails = append(emails, session.addEmails(11, 5, date("2024-11-02 15:15:00"), time.Minute)...)
	doTestSync(t, cfg, session, date("2024-11-02 15:25:00"), true)
	verifyMaildir(t, cfg.maildir, emails)

	// Add 5 more messages, wait a few days, and sync.
	emails = append(emails, session.addEmails(15, 5, date("2024-11-02 15:30:00"), time.Minute)...)
	doTestSync(t, cfg, session, date("2024-11-06 00:00:00"), true)
	verifyMaildir(t, cfg.maildir, emails)

	// Add 5 old emails to the server and check that they aren't downloaded.
	session.addEmails(20, 5, date("2023-11-02 00:00:00"), time.Minute)
	doTestSync(t, cfg, session, date("2024-11-06 00:05:00"), true)
	verifyMaildir(t, cfg.maildir, emails)
}

func TestSync_QueryError(t *testing.T) {
	cfg, _ := makeTestConfig(t)
	session := newTestSession()

	// Do a no-op sync to set the last sync time.
	doTestSync(t, cfg, session, date("2024-11-02 14:00:00"), true)

	// Add 10 emails, but make the query fail after the first 3 are returned.
	emails := session.addEmails(1, 10, date("2024-11-02 14:15:00"), time.Minute)
	session.errAfter = 3
	doTestSync(t, cfg, session, date("2024-11-02 15:00:00"), false)
	verifyMaildir(t, cfg.maildir, emails[:3])

	// Do another sync that gets a few more messages.
	session.errAfter = 6
	doTestSync(t, cfg, session, date("2024-11-02 15:00:20"), false)
	verifyMaildir(t, cfg.maildir, emails[:6])

	// Do a successful sync a day later to get the rest of the messages.
	session.errAfter = 0
	doTestSync(t, cfg, session, date("2024-11-03 15:00:00"), true)
	verifyMaildir(t, cfg.maildir, emails)
}

func TestSync_DownloadError(t *testing.T) {
	cfg, _ := makeTestConfig(t)
	session := newTestSession()

	// Do a no-op sync to set the last sync time.
	doTestSync(t, cfg, session, date("2024-11-02 14:00:00"), true)

	// Add 10 emails, but make the fifth download fail.
	emails := session.addEmails(1, 10, date("2024-11-02 14:15:00"), time.Minute)
	session.blobErrs[emails[4].BlobID] = struct{}{}
	doTestSync(t, cfg, session, date("2024-11-02 14:30:00"), false)
	verifyMaildir(t, cfg.maildir, emails[:4])

	// Do another sync a bit more than a day later to get all the messages.
	clear(session.blobErrs)
	doTestSync(t, cfg, session, date("2024-11-03 15:00:00"), true)
	verifyMaildir(t, cfg.maildir, emails)
}

func TestSync_TimeFilter(t *testing.T) {
	cfg, _ := makeTestConfig(t)
	session := newTestSession()

	// Add 10 emails, but pass a min time such that only the last 5 are downloaded.
	emails := session.addEmails(1, 10, date("2024-11-02 14:10:00"), time.Minute)
	cfg.minTime = date("2024-11-02 14:15:00")
	doTestSync(t, cfg, session, date("2024-11-02 14:30:00"), true)
	verifyMaildir(t, cfg.maildir, emails[5:])

	// Sync again about a day later to update the last sync time.
	doTestSync(t, cfg, session, date("2024-11-03 14:00:00"), true)
	verifyMaildir(t, cfg.maildir, emails[5:])

	// Doing a regular sync without a min time shouldn't return the earlier emails now.
	cfg.minTime = time.Time{}
	doTestSync(t, cfg, session, date("2024-11-03 14:05:00"), true)
	verifyMaildir(t, cfg.maildir, emails[5:])

	// Pick up the missing messages. A max time needs to be set to avoid duplicates.
	cfg.minTime = date("2024-11-02 14:10:00")
	cfg.maxTime = date("2024-11-02 14:15:00")
	doTestSync(t, cfg, session, date("2024-11-03 14:10:00"), true)
	verifyMaildir(t, cfg.maildir, emails)

	// Add some old and new messages and clear the min and max times.
	// We should get the new messages but not the old ones.
	session.addEmails(10, 5, date("2024-11-02 14:20:00"), time.Minute)
	emails = append(emails, session.addEmails(15, 5, date("2024-11-03 14:15:00"), time.Minute)...)
	cfg.minTime = time.Time{}
	cfg.maxTime = time.Time{}
	doTestSync(t, cfg, session, date("2024-11-03 14:12:00"), true)
	verifyMaildir(t, cfg.maildir, emails)
}

func TestSync_MailboxFilter(t *testing.T) {
	cfg, _ := makeTestConfig(t)
	session := newTestSession()

	// Add 5 messages in the "Inbox" mailbox and 5 more in "Sent".
	emails := session.addEmails(1, 5, date("2024-11-02 14:00:00"), time.Minute, "Inbox")
	session.addEmails(5, 10, date("2024-11-02 14:05:00"), time.Minute, "Sent")
	cfg.mailboxName = "Inbox"
	doTestSync(t, cfg, session, date("2024-11-03 14:15:00"), true)
	verifyMaildir(t, cfg.maildir, emails)
}

func TestSync_List(t *testing.T) {
	cfg, stdout := makeTestConfig(t)
	cfg.list = true
	session := newTestSession()

	session.addEmails(1, 5, dateLocal("2024-11-02 14:00:00"), time.Minute)
	doTestSync(t, cfg, session, dateLocal("2024-11-02 14:10:00"), true)
	if _, err := os.Stat(cfg.maildir); err == nil {
		t.Errorf("Maildir %v incorrectly created when listing", cfg.maildir)
	}

	// go-cmp's diff output for strings is atrocious. :-(
	want := strings.TrimLeft(`
M01  2024-11-02 14:00:00  01@example.org        Message 01
M02  2024-11-02 14:01:00  02@example.org        Message 02
M03  2024-11-02 14:02:00  03@example.org        Message 03
M04  2024-11-02 14:03:00  04@example.org        Message 04
M05  2024-11-02 14:04:00  05@example.org        Message 05
`, "\n")
	if got := stdout.String(); want != got {
		t.Error("Bad stdout:\nWant:\n" + want + "\nGot:\n" + got)
	}
}

// makeTestConfig returns a new base syncConfig for a test to use.
func makeTestConfig(t *testing.T) (syncConfig, *bytes.Buffer) {
	var stdout bytes.Buffer
	return syncConfig{
		dbPath:        filepath.Join(t.TempDir(), "state.db"),
		maildir:       filepath.Join(t.TempDir(), "mail"),
		stdout:        &stdout,
		queryChanSize: 1,
	}, &stdout
}

// date parses a UTC time in "YYYY-MM-DD HH:MM:SS" format.
func date(s string) time.Time {
	tm, err := time.Parse(time.DateTime, s)
	if err != nil {
		panic(fmt.Sprint("Invalid time: ", err))
	}
	return tm
}

// dateLocal parses a local time in "YYYY-MM-DD HH:MM:SS" format.
func dateLocal(s string) time.Time {
	tm, err := time.ParseInLocation(time.DateTime, s, time.Local)
	if err != nil {
		panic(fmt.Sprint("Invalid time: ", err))
	}
	return tm
}

// doTestSync calls sync() using a copy of cfg with the supplied startTime.
func doTestSync(t *testing.T, cfg syncConfig, s *testSession, startTime time.Time, wantSuccess bool) {
	t.Helper()
	cfg.startTime = startTime
	if cmdErr := sync(context.Background(), cfg, s); wantSuccess && cmdErr != nil {
		t.Fatalf("Sync failed with exit code %v: %v", cmdErr.code, cmdErr.msg)
	} else if !wantSuccess && cmdErr == nil {
		t.Fatal("Sync unexpectedly succeeded")
	}
}

// testSession implements the session interface for testing.
type testSession struct {
	emails    map[string]jmap.Email          // ID -> email
	blobs     map[string]string              // BlobID -> ID
	mailboxes map[string]map[string]struct{} // mailbox name -> set of email IDs

	errAfter int                 // if non-zero, report error after sending this many query results
	blobErrs map[string]struct{} // report errors when downloading these blobIDs
}

func newTestSession() *testSession {
	return &testSession{
		emails:    make(map[string]jmap.Email),
		blobs:     make(map[string]string),
		mailboxes: make(map[string]map[string]struct{}),
		blobErrs:  make(map[string]struct{}),
	}
}

// addEmail adds a single email message and associated blob to be returned by Query and Download.
func (s *testSession) addEmail(email jmap.Email, mailboxes ...string) {
	s.emails[email.ID] = email
	s.blobs[email.BlobID] = email.ID
	for _, mailbox := range mailboxes {
		m, ok := s.mailboxes[mailbox]
		if !ok {
			m = make(map[string]struct{})
			s.mailboxes[mailbox] = m
		}
		m[email.ID] = struct{}{}
	}
}

// addEmails adds n emails received step apart with IDs starting at base.
// The emails are returned.
func (s *testSession) addEmails(base, n int, start time.Time, step time.Duration, mailboxes ...string) []jmap.Email {
	var emails []jmap.Email
	for i := range n {
		id := base + i
		idStr := fmt.Sprintf("%02d", id)
		blobID := "B" + idStr
		email := jmap.Email{
			ID:         "M" + idStr,
			BlobID:     blobID,
			From:       []jmap.EmailAddress{{Email: idStr + "@example.org", Name: "Sender " + idStr}},
			Subject:    "Message " + idStr,
			ReceivedAt: start.Add(time.Duration(i) * step),
			Size:       uint64(len(blobID)),
		}
		emails = append(emails, email)
		s.addEmail(email, mailboxes...)
	}
	return emails
}

func (s *testSession) Query(ctx context.Context, cfg jmap.QueryConfig, ch chan<- jmap.Email) error {
	defer close(ch)
	var emails []jmap.Email
	for _, email := range s.emails {
		if (!cfg.After.IsZero() && cfg.After.After(email.ReceivedAt)) ||
			(!cfg.Before.IsZero() && !email.ReceivedAt.Before(cfg.Before)) ||
			(cfg.MailboxName != "" && !setContains(s.mailboxes[cfg.MailboxName], email.ID)) {
			continue
		}
		emails = append(emails, email)
	}
	sort.Slice(emails, func(i, j int) bool { return emails[i].ReceivedAt.Before(emails[j].ReceivedAt) })
	if cfg.TotalEmailsOut != nil {
		*cfg.TotalEmailsOut = uint64(len(emails))
	}
	for i, email := range emails {
		ch <- email
		if i+1 == s.errAfter {
			return errors.New("intentional error")
		}
	}
	return nil
}

func (s *testSession) Download(ctx context.Context, blobID string) (io.ReadCloser, error) {
	if _, ok := s.blobs[blobID]; !ok {
		return nil, errors.New("not found")
	}
	if setContains(s.blobErrs, blobID) {
		return nil, errors.New("intentional error")
	}
	return (*stringsReaderCloser)(strings.NewReader(blobID)), nil
}

// verifyMaildir verifies that dir contains exactly the supplied emails.
// Each file in dir must contain the corresponding message's BlobID.
func verifyMaildir(t *testing.T, dir string, emails []jmap.Email) {
	t.Helper()

	newDir := filepath.Join(dir, "new")
	entries, err := os.ReadDir(newDir)
	if err != nil {
		t.Fatal("Failed reading Maildir:", err)
	}
	maildirIDs := make(map[string]struct{}, len(entries))
	dupeIDs := make(map[string]struct{})
	for _, ent := range entries {
		if !ent.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(newDir, ent.Name()))
		if err != nil {
			t.Fatal("Failed reading message:", err)
		}
		if _, ok := maildirIDs[string(b)]; ok {
			dupeIDs[string(b)] = struct{}{}
		}
		maildirIDs[string(b)] = struct{}{}
	}

	emailIDs := make(map[string]struct{}, len(emails))
	for _, email := range emails {
		emailIDs[email.BlobID] = struct{}{}
	}

	if !maps.Equal(maildirIDs, emailIDs) {
		t.Fatalf("%v doesn't contain desired messages:\nWant: %v\nGot:  %v",
			newDir, stringifySet(emailIDs), stringifySet(maildirIDs))
	}
	if len(dupeIDs) > 0 {
		t.Fatalf("%v contains duplicate messages: %v", newDir, stringifySet(dupeIDs))
	}
}

type stringsReaderCloser strings.Reader // sigh

func (r *stringsReaderCloser) Close() error               { return nil }
func (r *stringsReaderCloser) Read(b []byte) (int, error) { return (*strings.Reader)(r).Read(b) }

// stringifySet converts s to a sorted, space-separated string.
func stringifySet(s map[string]struct{}) string {
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}
