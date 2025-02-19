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
	ID         string    `json:"id"`
	BlobID     string    `json:"blobId"`
	Subject    string    `json:"subject"`
	ReceivedAt time.Time `json:"receivedAt"`
}

// Query fetches information about messages with "receivedAt" dates between after and before
// (either of which may be the zero value). Results are written to ch, which is always closed
// before returning.
func (s *Session) Query(ctx context.Context, after, before time.Time, ch chan<- MessageInfo) error {
	defer close(ch)

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
		if !after.IsZero() || !before.IsZero() {
			m := make(map[string]any)
			if !after.IsZero() {
				m["after"] = after.UTC().Format(time.RFC3339)
			}
			if !before.IsZero() {
				m["before"] = before.UTC().Format(time.RFC3339)
			}
			queryArgs["filter"] = m
		}

		jreq := request{
			Using: []string{
				"urn:ietf:params:jmap:core",
				"urn:ietf:params:jmap:mail",
			},
			MethodCalls: []invocation{
				{
					Name: "Email/query",
					Args: queryArgs,
					ID:   "0",
				},
				{
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
			},
		}

		b, err := json.Marshal(&jreq)
		if err != nil {
			return err
		}
		res, err := s.call(ctx, http.MethodPost, s.data.APIURL, bytes.NewReader(b))
		if err != nil {
			return err
		}
		defer res.Body.Close()

		var jres response
		if err := json.NewDecoder(res.Body).Decode(&jres); err != nil {
			return err
		} else if n := len(jres.MethodResponses); n != 2 {
			return fmt.Errorf("got %v method response(s); expected 2", n)
		} else if id1, id2 := jres.MethodResponses[1].ID, jreq.MethodCalls[1].ID; id1 != id2 {
			return fmt.Errorf("second method response was for %q; expected %q", id1, id2)
		}
		var list []MessageInfo
		if err := unmarshalAny(jres.MethodResponses[1].Args["list"], &list); err != nil {
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
