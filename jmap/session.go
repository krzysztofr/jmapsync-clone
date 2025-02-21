// Copyright 2025 Daniel Erat.
// All rights reserved.

// Package jmap fetches email messages from a JMAP Mail server.
//
//	https://jmap.io/spec-core.html, https://datatracker.ietf.org/doc/rfc8620/
//	https://jmap.io/spec-mail.html, https://datatracker.ietf.org/doc/rfc8621/
package jmap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codeberg.org/derat/jmapsync/vlog"
)

const (
	coreURN = "urn:ietf:params:jmap:core"
	mailURN = "urn:ietf:params:jmap:mail"

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
		PrimaryAccounts map[string]string `json:"primaryAccounts"`
		Capabilities    struct {
			Core struct {
				MaxSizeRequest        uint64 `json:"maxSizeRequest"`
				MaxConcurrentRequests uint64 `json:"maxConcurrentRequests"`
				MaxCallsInRequest     uint64 `json:"maxCallsInRequest"`
				MaxObjectsInGet       uint64 `json:"maxObjectsInGet"`
			} `json:"urn:ietf:params:jmap:core"`
		}
	}
}

// NewSession returns a new JMAP session initialized from the supplied JMAP Session resource URL.
// The supplied token is sent in an Authorization bearer header.
func NewSession(ctx context.Context, url, token string) (*Session, error) {
	s := Session{token: token}
	res, err := s.sendHTTPRequest(ctx, http.MethodGet, url, nil)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if err := json.NewDecoder(res.Body).Decode(&s.data); err != nil {
		return nil, err
	}
	if s.accountID = s.data.PrimaryAccounts[mailURN]; s.accountID == "" {
		return nil, fmt.Errorf("no primary account for %v", mailURN)
	}
	return &s, nil
}

// Email describes an email message on the server.
type Email struct {
	// ID uniquely identifies the message.
	ID string `json:"id"`
	// BlobID uniquely identifies the message's content.
	BlobID string `json:"blobId"`
	// Size contains the message's raw data size in octets.
	Size uint64 `json:"size"`
	// ReceivedAt is the time the message was received by the server.
	ReceivedAt time.Time `json:"receivedAt"`
	// From contains the message's sender.
	From []EmailAddress `json:"from"`
	// Subject contains the message's subject.
	Subject string `json:"subject"`
}

// EmailAddress describes an email address in a message header.
type EmailAddress struct {
	// Email contains the actual email address, e.g. "user@example.org".
	Email string `json:"email"`
	// Name contains the name associated with the address, if any.
	Name string `json:"name"`
}

// QueryConfig configures Query's behavior.
type QueryConfig struct {
	// After is an inclusive lower bound for messages' "receivedAt" dates.
	After time.Time
	// Before is an exclusive upper bound for messages' "receivedAt" dates.
	Before time.Time
	// MailboxName is the name of a mailbox that messages must be in, e.g. "Inbox" or "Sent".
	MailboxName string
	// TotalEmailsOut is set (if non-nil) to the total number of messages before any writes to ch occur.
	TotalEmailsOut *uint64
	// GetDetails controls whether the ReceivedAt, From, and Subject fields are fetched.
	GetDetails bool
}

// Query fetches information about messages.
// Results are written to ch, which is always closed before returning.
func (s *Session) Query(ctx context.Context, cfg QueryConfig, ch chan<- Email) error {
	defer close(ch)

	var mailboxID string
	if cfg.MailboxName != "" {
		var err error
		if mailboxID, err = s.getMailboxID(ctx, cfg.MailboxName); err != nil {
			return fmt.Errorf("get mailbox ID: %w", err)
		}
		vlog.Logf(ctx, "Mailbox %q has ID %v", cfg.MailboxName, mailboxID)
	}

	var pos uint64
	for {
		queryArgs := map[string]any{
			"accountId": s.accountID,
			"sort": []map[string]any{{
				"property":    "receivedAt",
				"isAscending": true,
			}},
			"limit":          min(s.data.Capabilities.Core.MaxObjectsInGet, maxQueryBatchSize),
			"position":       pos,
			"calculateTotal": true,
		}

		filter := make(map[string]any)
		if !cfg.After.IsZero() {
			filter["after"] = cfg.After.UTC().Format(time.RFC3339)
		}
		if !cfg.Before.IsZero() {
			filter["before"] = cfg.Before.UTC().Format(time.RFC3339)
		}
		if mailboxID != "" {
			filter["inMailbox"] = mailboxID
		}
		if len(filter) > 0 {
			queryArgs["filter"] = filter
		}

		props := []string{"blobId", "size"}
		if cfg.GetDetails {
			props = append(props, "receivedAt", "from", "subject")
		}

		vlog.Logf(ctx, "Sending request for position %v with filter %v", pos, filter)
		res, err := s.sendJMAPRequest(
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
					"properties": props,
				},
				ID: "1",
			},
		)
		if err != nil {
			return err
		}

		qargs := res.MethodResponses[0].Args
		var rpos uint64
		if err := unmarshalAny(qargs["position"], &rpos); err != nil {
			return err
		}
		if rpos != pos {
			return fmt.Errorf("asked for position %v but got %v", pos, rpos)
		}

		var total uint64
		if err := unmarshalAny(qargs["total"], &total); err != nil {
			return err
		}
		if cfg.TotalEmailsOut != nil && pos == 0 {
			*cfg.TotalEmailsOut = total
		}

		var list []Email
		if err := unmarshalAny(res.MethodResponses[1].Args["list"], &list); err != nil {
			return err
		}
		if len(list) == 0 {
			vlog.Logf(ctx, "Got 0 emails")
			break
		}
		vlog.Logf(ctx, "Got %v email(s) (%v-%v/%v)", len(list), pos+1, pos+uint64(len(list)), total)
		for _, em := range list {
			ch <- em
		}
		pos += uint64(len(list))
		if pos >= total {
			break
		}
	}

	return nil
}

// getMailboxID returns the ID for the mailbox with the specified name.
func (s *Session) getMailboxID(ctx context.Context, name string) (string, error) {
	res, err := s.sendJMAPRequest(ctx, invocation{
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

// sendHTTPRequest makes an HTTP request for the specified URL using s.token.
// If an http.Response is returned, the caller is responsibly for closing its Body member regardless
// of whether the returned error is nil or non-nil.
func (s *Session) sendHTTPRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return res, err
	}
	if res.StatusCode != http.StatusOK {
		return res, fmt.Errorf("server returned %v: %v", res.StatusCode, res.Status)
	}
	return res, nil
}

// request describes a JMAP request.
type request struct {
	Using       []string     `json:"using"`
	MethodCalls []invocation `json:"methodCalls"`
}

type problemDetails struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// sendJMAPRequest sends a JMAP Mail request with the supplied method calls.
func (s *Session) sendJMAPRequest(ctx context.Context, methodCalls ...invocation) (*response, error) {
	jreq := request{
		Using:       []string{coreURN, mailURN},
		MethodCalls: methodCalls,
	}
	b, err := json.Marshal(&jreq)
	if err != nil {
		return nil, err
	}
	res, err := s.sendHTTPRequest(ctx, http.MethodPost, s.data.APIURL, bytes.NewReader(b))
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		// Try to extract details from the body.
		if res != nil {
			var prob problemDetails
			if derr := json.NewDecoder(res.Body).Decode(&prob); derr == nil {
				return nil, fmt.Errorf("%w (detail: %q)", err, prob.Detail)
			}
		}
		return nil, err
	}

	var jres response
	if err := json.NewDecoder(res.Body).Decode(&jres); err != nil {
		return nil, err
	}
	if n1, n2 := len(jreq.MethodCalls), len(jres.MethodResponses); n1 != n2 {
		return &jres, fmt.Errorf("sent %v method calls(s) but got %v response(s)", n1, n2)
	}
	for i := range jreq.MethodCalls {
		if id1, id2 := jreq.MethodCalls[i].ID, jres.MethodResponses[i].ID; id1 != id2 {
			return &jres, fmt.Errorf("method call and response IDs differ (%q vs. %q)", id1, id2)
		}
		if mr := jres.MethodResponses[i]; mr.Name == "error" {
			// Get the "type" field for more details per RFC 8620 3.6.2.
			errType, _ := mr.Args["type"]
			return &jres, fmt.Errorf("method call %v failed: %v", mr.ID, errType)
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
	res, err := s.sendHTTPRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}
