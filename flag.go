// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"strings"
	"time"
)

// repeatedFlag can be specified multiple times to supply string values.
type repeatedFlag []string

func (rf *repeatedFlag) String() string { return strings.Join(*rf, ",") }
func (rf *repeatedFlag) Set(v string) error {
	*rf = append(*rf, v)
	return nil
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
