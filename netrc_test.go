// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadNetrcMachine(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".netrc")
	if err := os.WriteFile(p, []byte(strings.TrimLeft(`
machine example.org login user password secret
machine foo.example.org login me
machine bar.example.org login me2 password blah account blegh
default login anonymous
`, "\n")), 0644); err != nil {
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
