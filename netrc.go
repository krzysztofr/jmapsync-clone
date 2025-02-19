// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"bytes"
	"errors"
	"os"
	"regexp"
)

// readNetrcMachine returns the .netrc entry for the specified machine name.
// nil is returned if the machine is not found.
//
//	https://linux.die.net/man/5/netrc
//	https://www.gnu.org/software/inetutils/manual/html_node/The-_002enetrc-file.html
func readNetrcMachine(p string, machine string) (*netrcMachine, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	tokens := netrcSpaceRegexp.Split(string(bytes.TrimSpace(b)), -1)

	var tokenIdx int
	readToken := func() (string, bool) {
		if tokenIdx >= len(tokens) {
			return "", false
		}
		token := tokens[tokenIdx]
		tokenIdx++
		return token, true
	}

	var cur *netrcMachine

	for {
		token, ok := readToken()
		if !ok {
			break
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
			cur.machine, ok = readToken()
		case "default":
			// Don't consume another token.
		case "login":
			cur.login, ok = readToken()
		case "password":
			cur.password, ok = readToken()
		case "account":
			cur.account, ok = readToken()
		default:
			return nil, errors.New("invalid token")
		}
		if !ok {
			return nil, errors.New("ran out of tokens")
		}
	}
	if cur != nil && cur.machine == machine {
		return cur, nil
	}
	return nil, nil
}

type netrcMachine struct {
	machine  string
	login    string
	password string
	account  string
}

var netrcSpaceRegexp = regexp.MustCompile(`[ \t\n]+`)
