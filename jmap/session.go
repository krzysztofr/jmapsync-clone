// Copyright 2025 Daniel Erat.
// All rights reserved.

// Package jmap fetches email messages from a JMAP Mail server.
//
//	https://datatracker.ietf.org/doc/rfc8620/
//	https://datatracker.ietf.org/doc/rfc8621/
package jmap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	maxQueryBatchSize = 500
)

var reqUsing = []string{
	"urn:ietf:params:jmap:core",
	"urn:ietf:params:jmap:mail",
}

// Session is a JMAP Mail session.
type Session struct {
	// accountID is the account ID returned by the JMAP Session resource URL.
	accountID string
	// token contains the bearer token included in HTTP requests.
	token string
	// data contains raw session data from the JMAP Session resource URL.
	data struct {
		APIURL      string `json:"apiUrl"`
		DownloadURL string `json:"downloadUrl"`
		Accounts    map[string]struct {
			Name       string `json:"name"`
			IsPersonal bool   `json:"isPersonal"`
			IsReadOnly bool   `json:"isReadOnly"`
		}
		Capabilities struct {
			Core struct {
				MaxObjectsInGet int `json:"maxObjectsInGet"`
			} `json:"urn:ietf:params:jmap:core"`
		}
	}
}

// NewSession returns a new JMAP session initialized from the supplied JMAP Session resource URL.
// The supplied token is sent in an Authorization bearer header.
func NewSession(ctx context.Context, url, token string) (*Session, error) {
	s := Session{token: token}
	res, err := s.call(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&s.data); err != nil {
		return nil, err
	}
	if len(s.data.Accounts) != 1 {
		return nil, fmt.Errorf("got %v accounts; want 1", len(s.data.Accounts))
	}
	for k := range s.data.Accounts {
		s.accountID = k
		break
	}
	return &s, nil
}

// MessageInfo describes an email message on the server.
type MessageInfo struct {
	// ID uniquely identifies the message.
	ID string `json:"id"`
	// BlobID uniquely identifies the message's content.
	BlobID string `json:"blobId"`
	// Subject contains the message's subject.
	Subject string `json:"subject"`
	// ReceivedAt is the time the message was received by the server.
	ReceivedAt time.Time `json:"receivedAt"`
}

// QueryFilter configures which messages are returned by Query.
type QueryFilter struct {
	// After is a lower bound for messages' "receivedAt" dates.
	After time.Time
	// Before is an upper bound for messages' "receivedAt" dates.
	Before time.Time
	// MailboxName is the name of a mailbox that messages must be in, e.g. "Inbox" or "Sent".
	MailboxName string
}

// Query fetches information about messages matched by QueryFilter.
// Results are written to ch, which is always closed before returning.
func (s *Session) Query(ctx context.Context, qf QueryFilter, ch chan<- MessageInfo) error {
	defer close(ch)

	var mailboxID string
	if qf.MailboxName != "" {
		var err error
		if mailboxID, err = s.getMailboxID(ctx, qf.MailboxName); err != nil {
			return fmt.Errorf("get mailbox ID: %w", err)
		}
	}

	var pos int
	for {
		log.Printf("Querying at position %v", pos)

		queryArgs := map[string]any{
			"accountId": s.accountID,
			"sort": []map[string]any{{
				"property":    "receivedAt",
				"isAscending": true,
			}},
			"limit":    min(s.data.Capabilities.Core.MaxObjectsInGet, maxQueryBatchSize),
			"position": pos,
		}

		filter := make(map[string]any)
		if !qf.After.IsZero() {
			filter["after"] = qf.After.UTC().Format(time.RFC3339)
		}
		if !qf.Before.IsZero() {
			filter["before"] = qf.Before.UTC().Format(time.RFC3339)
		}
		if mailboxID != "" {
			filter["inMailbox"] = mailboxID
		}
		if len(filter) > 0 {
			queryArgs["filter"] = filter
		}

		res, err := s.sendRequest(
			ctx,
			invocation{
				Name: "Email/query",
				Args: queryArgs,
				ID:   "0",
			},
			invocation{
				Name: "Email/get",
				Args: map[string]any{
					"accountId": s.accountID,
					"#ids": map[string]any{
						"name":     "Email/query",
						"path":     "/ids/*",
						"resultOf": "0",
					},
					"properties": []string{"blobId", "receivedAt", "subject"},
				},
				ID: "1",
			},
		)
		if err != nil {
			return err
		}
		var list []MessageInfo
		if err := unmarshalAny(res.MethodResponses[1].Args["list"], &list); err != nil {
			return err
		}
		if len(list) == 0 {
			break
		}
		for _, msg := range list {
			ch <- msg
		}
		pos += len(list)
	}

	return nil
}

// getMailboxID returns the ID for the mailbox with the specified name.
func (s *Session) getMailboxID(ctx context.Context, name string) (string, error) {
	res, err := s.sendRequest(ctx, invocation{
		Name: "Mailbox/query",
		Args: map[string]any{
			"accountId": s.accountID,
			"filter":    map[string]any{"name": name},
		},
		ID: "1",
	})
	if err != nil {
		return "", err
	}
	var ids []string
	if err := unmarshalAny(res.MethodResponses[0].Args["ids"], &ids); err != nil {
		return "", err
	} else if len(ids) != 1 {
		return "", fmt.Errorf("got %v IDs; want 1", len(ids))
	}
	return ids[0], nil
}

// unmarshalAny is a dumb helper function that marshals src to JSON and then unmarshals the
// resulting bytes into dst. There has to be a better way of doing this, but I think that the
// core problem is that I want invocation.Args to be a map[string]any when request is using it
// and a map[string]json.RawMessage when response is using it.
func unmarshalAny(src any, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &dst)
}

// call makes an HTTP call to the specified URL using s.token.
// The caller is responsible for closing the response's Body iff the returned error is non-nil.
func (s *Session) call(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %v: %v", res.StatusCode, res.Status)
	}
	return res, nil
}

// request describes a JMAP request.
type request struct {
	Using       []string     `json:"using"`
	MethodCalls []invocation `json:"methodCalls"`
}

// sendRequest sends a JMAP Mail request with the supplied method calls.
func (s *Session) sendRequest(ctx context.Context, methodCalls ...invocation) (*response, error) {
	jreq := request{
		Using: []string{
			"urn:ietf:params:jmap:core",
			"urn:ietf:params:jmap:mail",
		},
		MethodCalls: methodCalls,
	}
	b, err := json.Marshal(&jreq)
	if err != nil {
		return nil, err
	}
	res, err := s.call(ctx, http.MethodPost, s.data.APIURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var jres response
	if err := json.NewDecoder(res.Body).Decode(&jres); err != nil {
		return nil, err
	}
	if n1, n2 := len(jreq.MethodCalls), len(jres.MethodResponses); n1 != n2 {
		return nil, fmt.Errorf("send %v method calls(s) but got %v response(s)", n1, n2)
	}
	for i := range jreq.MethodCalls {
		if id1, id2 := jreq.MethodCalls[i].ID, jres.MethodResponses[i].ID; id1 != id2 {
			return nil, fmt.Errorf("method call and response IDs differ (%q vs. %q)", id1, id2)
		}
	}
	return &jres, nil
}

// response describes a JMAP response.
type response struct {
	MethodResponses []invocation `json:"methodResponses"`
}

// invocation describes a JMAP method call or response.
type invocation struct {
	Name string
	Args map[string]any
	ID   string
}

func (inv *invocation) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{inv.Name, inv.Args, inv.ID})
}

func (inv *invocation) UnmarshalJSON(b []byte) error {
	dst := []any{&inv.Name, &inv.Args, &inv.ID}
	return json.Unmarshal(b, &dst)
}

// Download fetches the message identified by blobID.
// The caller must close the returned io.ReadCloser iff error is nil.
func (s *Session) Download(ctx context.Context, blobID string) (io.ReadCloser, error) {
	u := s.data.DownloadURL
	for key, val := range map[string]string{
		"{accountId}": s.accountID,
		"{blobId}":    blobID,
		"{type}":      "application/octet-stream",
		"{name}":      "email",
	} {
		if !strings.Contains(u, key) {
			return nil, fmt.Errorf("%q not present in download URL %v", key, u)
		}
		u = strings.ReplaceAll(u, key, val)
	}
	res, err := s.call(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}
