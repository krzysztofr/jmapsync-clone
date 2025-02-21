// Copyright 2025 Daniel Erat.
// All rights reserved.

package netrc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const testInput = `machine example.org login user password secret
machine foo.example.org login me macdef init
  cd /foo/bar
  pwd
  bye

macdef another
mget *.c
bye

machine bar.example.org login me2 password blah account blegh
default login anonymous
`

var testMachines = []Machine{
	{Machine: "example.org", Login: "user", Password: "secret"},
	{Machine: "foo.example.org", Login: "me", Macros: map[string]string{
		"init":    "cd /foo/bar\n  pwd\n  bye\n",
		"another": "mget *.c\nbye\n",
	}},
	{Machine: "bar.example.org", Login: "me2", Password: "blah", Account: "blegh"},
	{Login: "anonymous"},
}

func TestParse(t *testing.T) {
	if got, err := Parse(strings.NewReader(testInput)); err != nil {
		t.Error("Parse failed:", err)
	} else if diff := cmp.Diff(testMachines, got); diff != "" {
		t.Error("Parse returned incorrect data:\n" + diff)
	}
}

func TestReadMachine(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".netrc")
	if err := os.WriteFile(p, []byte(testInput), 0644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		machine string
		want    *Machine
	}{
		{"example.org", &testMachines[0]},
		{"foo.example.org", &testMachines[1]},
		{"bar.example.org", &testMachines[2]},
		{"", &testMachines[3]},
		{"bogus", nil},
	} {
		if got, err := ReadMachine(p, tc.machine); err != nil {
			t.Errorf("ReadMachine(%q, %q) failed: %v", p, tc.machine, err)
		} else if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ReadMachine(%q, %q) = %+v; want %+v", p, tc.machine, got, tc.want)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(testInput)
	// Examples from https://everything.curl.dev/usingcurl/netrc.html:
	f.Add("machine example.com\nlogin daniel\npassword qwerty")
	f.Add("machine example.com login daniel password qwerty")

	f.Fuzz(func(t *testing.T, input string) {
		if got, err := Parse(strings.NewReader(input)); len(got) > 0 && err != nil {
			t.Errorf("Got machine(s) %+v but also got error %v", got, err)
		}
	})
}
