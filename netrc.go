// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
)

// readNetrcMachine returns the .netrc entry for the specified machine name.
//
//	https://linux.die.net/man/5/netrc
//	https://www.gnu.org/software/inetutils/manual/html_node/The-_002enetrc-file.html
func readNetrcMachine(p string, machine string) (*netrcMachine, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	tokens := netrcSpaceRegexp.Split(string(b), -1)

	var i int
	var readErr error
	readToken := func() string {
		if readErr != nil {
			return ""
		}
		if i >= len(tokens) {
			readErr = errors.New("ran out of tokens")
			return ""
		}
		defer func() { i++ }()
		return tokens[i]
	}

	var cur *netrcMachine

	for {
		token := readToken()
		if readErr != nil {
			return nil, readErr
		}

		if token == "macdef" {
			// TODO: Ought to skip ahead to the next double newline here.
			return nil, errors.New("macdef unsupported")
		}

		// End the current machine (if any) and start a new one.
		if token == "machine" || token == "default" {
			if cur != nil && cur.machine == machine {
				return cur, nil
			}
			cur = &netrcMachine{}
		} else if cur == nil {
			return nil, errors.New("not in machine")
		}
		switch token {
		case "machine":
			cur.machine = readToken()
		case "default":
			// Don't consume another token.
		case "login":
			cur.login = readToken()
		case "password":
			cur.password = readToken()
		case "account":
			cur.account = readToken()
		default:
			return nil, errors.New("invalid token")
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if cur != nil && cur.machine == machine {
		return cur, nil
	}
	return nil, fmt.Errorf("machine %q not found", machine)
}

type netrcMachine struct {
	machine  string
	login    string
	password string
	account  string
}

var netrcSpaceRegexp = regexp.MustCompile(`[ \t\n]+`)
