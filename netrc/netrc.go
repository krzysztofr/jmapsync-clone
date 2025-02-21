// Copyright 2025 Daniel Erat.
// All rights reserved.

// Package netrc parses .netrc files containing network credentials.
//
//	https://www.gnu.org/software/inetutils/manual/html_node/The-_002enetrc-file.html
package netrc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Machine describes a "machine" or "default" entry in a .netrc file.
type Machine struct {
	Machine  string
	Login    string
	Password string
	Account  string
	Macros   map[string]string
}

// Parse parses the supplied .netrc data and returns all machines.
func Parse(r io.Reader) ([]Machine, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var tokens, spaces []string // tokens and whitespace following each token
	text := string(bytes.TrimSpace(b))
	parts := nonspaceRegexp.FindAllStringIndex(text, -1)
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

	var machines []Machine
	for {
		token, _, ok := readToken()
		if !ok {
			break
		}

		// End the current machine (if any) and start a new one.
		if token == "machine" || token == "default" {
			machines = append(machines, Machine{})
		} else if len(machines) == 0 {
			return nil, errors.New("not in machine")
		}

		cur := &machines[len(machines)-1]
		switch token {
		case "machine":
			cur.Machine, _, ok = readToken()
		case "default":
			// Don't consume another token.
		case "login":
			cur.Login, _, ok = readToken()
		case "password":
			cur.Password, _, ok = readToken()
		case "account":
			cur.Account, _, ok = readToken()
		case "macdef":
			// This is poorly-documented FTP garbage that probably nobody uses.
			// It defines a macro with a specified name followed by random
			// data up to the next "null line" (consecutive newlines).
			var name, ws string
			if name, ws, ok = readToken(); !ok {
				break
			} else if !strings.Contains(ws, "\n") {
				return nil, errors.New("macdef must be followed by newline")
			}

			// Consume tokens until we find one followed by a newline. No idea how whitespace
			// should be handled here, and it seems like other implementations are buggy:
			// https://community.unix.com/t/multiple-macdefs-in-netrc/214780
			var data string
			for {
				var t string
				if t, ws, ok = readToken(); !ok {
					break // ended prematurely
				}
				data += t
				if strings.Contains(ws, "\n\n") {
					if cur.Macros == nil {
						cur.Macros = make(map[string]string)
					}
					cur.Macros[name] = data + "\n"
					break // ended normally
				}
				data += ws
			}
		default:
			return nil, fmt.Errorf("invalid token %q", token)
		}
		if !ok {
			return nil, fmt.Errorf("ran out of tokens in %q", token)
		}
	}
	return machines, nil
}

var nonspaceRegexp = regexp.MustCompile(`[^ \t\n]+`)

// Read reads and parses the specified .netrc file.
func Read(path string) ([]Machine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return Parse(f)
}

// ReadMachine reads and parses the specified .netrc file and returns the named machine.
// A nil pointer and nil error are returned if the machine is not found.
func ReadMachine(path, machine string) (*Machine, error) {
	machines, err := Read(path)
	if err != nil {
		return nil, err
	}
	for _, m := range machines {
		if m.Machine == machine {
			return &m, nil
		}
	}
	return nil, nil
}
