// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const netrcInput = `machine example.org login user password secret
machine foo.example.org login me macdef init
  cd /foo/bar
  pwd

machine bar.example.org login me2 password blah account blegh
default login anonymous
`

func TestReadNetrcMachine(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".netrc")
	if err := os.WriteFile(p, []byte(netrcInput), 0644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		machine string
		want    *netrcMachine
	}{
		{"example.org", &netrcMachine{machine: "example.org", login: "user", password: "secret"}},
		{"foo.example.org", &netrcMachine{machine: "foo.example.org", login: "me"}},
		{"bar.example.org", &netrcMachine{machine: "bar.example.org", login: "me2", password: "blah", account: "blegh"}},
		{"", &netrcMachine{login: "anonymous"}},
		{"bogus", nil},
	} {
		if got, err := readNetrcMachine(p, tc.machine); err != nil {
			t.Errorf("readNetrcMachine(%q, %q) failed: %v", p, tc.machine, err)
		} else if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("readNetrcMachine(%q, %q) = %+v; want %+v", p, tc.machine, got, tc.want)
		}
	}
}

func FuzzReadNetrcMachine(f *testing.F) {
	f.Add(netrcInput, "foo.example.org")
	// Examples from https://everything.curl.dev/usingcurl/netrc.html:
	f.Add("machine example.com\nlogin daniel\npassword qwerty", "example.com")
	f.Add("machine example.com login daniel password qwerty", "example.com")

	f.Fuzz(func(t *testing.T, netrc, machine string) {
		if got, err := readNetrcMachine(netrc, machine); got != nil && err != nil {
			t.Errorf("Got non-nil machine %+v but also got error %v", got, err)
		} else if got == nil && err == nil {
			t.Error("Got nil machine and nil error")
		} else if got != nil && got.machine != machine {
			t.Errorf("Requested machine %q but got %+v", machine, got)
		}
	})
}
