Note: This is my clone of jmapsync with modifications that I needed to run the script on my Windows 11, due to the following error:
```
cannot load SQLite driver: failed to open database: binary was compiled with 'CGO_ENABLED=0
```

In order to compile the program, you need to run first:
```
go get modernc.org/sqlite@latest
```

I took advice from [TutorialPedia.org article](https://www.tutorialpedia.org/blog/binary-was-compiled-with-cgo-enabled-0-go-sqlite3-requires-cgo-to-work-this-is-a-stub/#alternative-use-a-pure-go-sqlite-driver).

 I'm not expert in Go, so use it at your own risk. 
 -KR

# jmapsync

[![Build Status](https://storage.googleapis.com/derat-build-badges/8a908f0e-93f0-4cab-abf3-2497cd54768b.svg)](https://storage.googleapis.com/derat-build-badges/8a908f0e-93f0-4cab-abf3-2497cd54768b.html)

This is a command-line program that incrementally downloads email messages via
the [JMAP] protocol (e.g. from [Fastmail]) and writes them to a [Maildir]
directory.

[JMAP]: https://jmap.io/
[Fastmail]: https://www.fastmail.com/
[Maildir]: https://en.wikipedia.org/wiki/Maildir

I wrote
[more details about how I use jmapsync](https://www.erat.org/fastmail.html#download-messages)
as part of a long post about
[moving from Gmail to Fastmail](https://www.erat.org/fastmail.html).

## Installation

[Install Go] and run `go install` from the top of this repository to compile and
install the `jmapsync` executable to `$GOBIN` (or `$GOPATH/bin`, or
`$HOME/go/bin`).

[Install Go]: https://go.dev/dl/

## Usage

Run the `jmapsync` executable periodically to download new messages.

```
Usage: jmapsync [flag]...
Incrementally download email messages from a JMAP server.

  -db string
    	SQLite database for storing last sync state (default "$HOME/.jmapsync.db")
  -list
    	List all matching messages instead of syncing them
  -list-mailboxes
    	Print names of all mailboxes and exit
  -log-file string
    	Path to file where verbose logs will be written
  -mailbox string
    	Name of mailbox to sync (empty to sync all messages)
  -maildir string
    	Destination Maildir directory (created if it doesn't exist)
  -max-time value
    	Maximum received-at RFC 3339 time (exclusive, empty for no limit)
  -min-time value
    	Minimum received-at RFC 3339 time (inclusive, empty to get all since last sync)
  -netrc-file string
    	Path to .netrc file containing auth token (default "$HOME/.netrc")
  -not-only-mailbox value
    	Don't sync messages only in these mailboxes (can be repeated)
  -session-url string
    	JMAP Session resource URL (default "https://api.fastmail.com/jmap/session")
```

If you're using Fastmail, you can create an API token for read-only JMAP access
at <https://app.fastmail.com/settings/security/tokens>. Add a line to
`$HOME/.netrc` similar to the following:

```
machine api.fastmail.com login jmap password <api-token>
```

`jmapsync` just looks for a `machine` entry matching the hostname from
`-session-url`; the `login` value is not currently used.

A command to sync all new non-spam/trashed/draft messages from Fastmail might
look like the following:

```
jmapsync \
  -maildir /path/to/maildir \
  -not-only-mailbox Spam \
  -not-only-mailbox Trash \
  -not-only-mailbox Drafts
```

## Crash Safety & Duplicate Prevention

This fork implements crash-safe duplicate prevention for unexpected interruptions (Windows shutdown, Ctrl+C, power loss).

### Problem Analysis
The original implementation could create duplicate downloads when interrupted because message IDs were written to the database without proper durability guarantees (SQLite `synchronous=NORMAL` mode).

### Solution Options Evaluated

| Option | Approach | Pros | Cons |
|--------|----------|------|------|
| 1. Synchronous Writes | `synchronous=FULL` per message | Simple one-line fix, guaranteed durability | Very slow for initial 75k sync (hours of fsync overhead) |
| 2. Batched Commits | Commit every 50 messages with `synchronous=FULL` | Good performance (1,500 vs 75,000 commits), resumable sync, max 50 duplicates on crash | More complex implementation |
| 3. WAL Mode | `journal=WAL` with `synchronous=NORMAL` | Better performance | Additional files, still some crash risk |
| 4. Single Transaction | One transaction per sync | Perfectly consistent | Re-downloads ALL messages on any interruption |

### Chosen Solution: Option 2 (Batched Commits with Uncommitted File)

**Why:** Balances crash safety with performance for large initial syncs (75k messages) while maintaining resumability.

**How it works:**
- Database commits every 50 messages with `synchronous=FULL` (durable)
- Uncommitted IDs (max 50) tracked in `.jmapsync.db.uncommitted` file
- On startup: verifies uncommitted IDs against maildir file count
- If count matches: commits uncommitted IDs to database
- If mismatch: clears uncommitted file, relies on 1-hour overlap to re-download
- Result: Zero duplicates even with unexpected shutdowns

**Implementation details:**
- Uncommitted file automatically managed (created/cleared as needed)
- File count verification prevents database corruption from partial downloads
- Max 50 messages (0.07% of 75k) at risk on crash, automatically recovered

## Other notes

The original idea (and the realization of what an easy-to-use protocol JMAP is)
came from seeing the Python code in Nathan Grigg’s [Fastmail JMAP
backup](https://nathangrigg.com/2021/08/fastmail-backup/) blog post.
