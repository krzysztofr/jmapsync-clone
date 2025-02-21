// Copyright 2025 Daniel Erat.
// All rights reserved.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
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

	var tokens, spaces []string // tokens and whitespace following each token
	text := string(bytes.TrimSpace(b))
	parts := netrcNonspaceRegexp.FindAllStringIndex(text, -1)
	for i, part := range parts {
		tokens = append(tokens, text[part[0]:part[1]])
		if i < len(parts)-1 {
			spaces = append(spaces, text[part[1]:parts[i+1][0]])
		} else {
			spaces = append(spaces, "\n\n")
		}
	}

	var tokenIdx int
	readToken := func() (token, whitespace string, ok bool) {
		if tokenIdx >= len(tokens) {
			return "", "", false
		}
		defer func() { tokenIdx++ }()
		return tokens[tokenIdx], spaces[tokenIdx], true
	}

	var cur *netrcMachine

	for {
		token, _, ok := readToken()
		if !ok {
			break
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
			cur.machine, _, ok = readToken()
		case "default":
			// Don't consume another token.
		case "login":
			cur.login, _, ok = readToken()
		case "password":
			cur.password, _, ok = readToken()
		case "account":
			cur.account, _, ok = readToken()
		case "macdef":
			// This is FTP garbage that probably nobody uses.
			// It defines a macro with a specified name followed by random
			// data up to the next "null line" (consecutive newlines).
			var ws string
			if _, ws, ok = readToken(); !ok {
				break
			} else if !strings.Contains(ws, "\n") {
				return nil, errors.New("macdef must be followed by newline")
			}
			// Consume tokens until we find one followed by a newline.
			for {
				if _, ws, ok = readToken(); !ok || strings.Contains(ws, "\n\n") {
					break
				}
			}
		default:
			return nil, fmt.Errorf("invalid token %q", token)
		}
		if !ok {
			return nil, fmt.Errorf("ran out of tokens in %q", token)
		}
	}
	if cur != nil && cur.machine == machine {
		return cur, nil
	}
	return nil, nil
}

// netrcMachine describes a "machine" or "default" entry in a .netrc file.
type netrcMachine struct {
	machine  string
	login    string
	password string
	account  string
}

var netrcNonspaceRegexp = regexp.MustCompile(`[^ \t\n]+`)
