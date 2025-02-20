// Copyright 2025 Daniel Erat.
// All rights reserved.

// Package maildir delivers email messages to a maildir.
//
// See https://cr.yp.to/proto/maildir.html for more details.
package maildir

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// num is a monotonically-increasing delivery counter.
var num atomic.Uint64

// Maildir delivers messages to a maildir.
type Maildir struct {
	dir string // base directory
}

// New creates a maildir at dir if it doesn't already exist.
func New(dir string) (*Maildir, error) {
	for _, p := range []string{
		dir,
		filepath.Join(dir, "cur"),
		filepath.Join(dir, "new"),
		filepath.Join(dir, "tmp"),
	} {
		if fi, err := os.Stat(p); err == nil {
			if fi.IsDir() {
				continue
			}
			return nil, fmt.Errorf("%v exists as non-directory", p)
		}
		if err := os.Mkdir(p, 0700); err != nil {
			return nil, err
		}
	}
	return &Maildir{dir: dir}, nil
}

// Deliver reads a message from r and writes it to md.
// The message's full path is returned.
func (md *Maildir) Deliver(r io.Reader) (string, error) {
	// Create a file in the tmp subdir.
	host, err := os.Hostname()
	if err != nil {
		return "", err
	}
	host = strings.ReplaceAll(strings.ReplaceAll(host, "/", "\\057"), ":", "\\072")
	name := fmt.Sprintf("%v.%v_%v.%v", time.Now().Unix(), os.Getpid(), num.Add(1), host)
	tp := filepath.Join(md.dir, "tmp", name)
	tf, err := os.OpenFile(tp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}

	// Write the message to the tmp file.
	if _, err := io.Copy(tf, r); err != nil {
		tf.Close()
		os.Remove(tp)
		return "", err
	}
	if err := tf.Close(); err != nil {
		os.Remove(tp)
		return "", err
	}

	// Create a file in the new subdir.
	np := filepath.Join(md.dir, "new", name)
	nf, err := os.OpenFile(np, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		os.Remove(tp)
		return "", err
	}
	nf.Close()

	// Move the tmp file over the new one.
	if err := os.Rename(tp, np); err != nil {
		return "", err
	}
	return np, nil
}
