# jmapsync

[![Build Status](https://storage.googleapis.com/derat-build-badges/8a908f0e-93f0-4cab-abf3-2497cd54768b.svg)](https://storage.googleapis.com/derat-build-badges/8a908f0e-93f0-4cab-abf3-2497cd54768b.html)

This is a command-line program that incrementally downloads email messages via
the [JMAP] protocol (e.g. from [Fastmail]) and writes them to a [Maildir]
directory.

It's still a work in progress.

[JMAP]: https://jmap.io/
[Fastmail]: https://www.fastmail.com/
[Maildir]: https://en.wikipedia.org/wiki/Maildir

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
