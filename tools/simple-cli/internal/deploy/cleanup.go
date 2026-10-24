package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// The modes of a cleanup request. Describe changes nothing; execute deletes.
const (
	CleanupDescribe = "describe"
	CleanupExecute  = "execute"
)

// How a removal was confirmed: the user typed the app id, or --yes let it
// through.
const (
	CleanupConfirmedTyped = "typed"
	CleanupConfirmedFlag  = "flag"
)

// CleanupRequest names the tables and fields to delete. Fields are written
// "table.field". Expect, sent with CleanupExecute only, is the fingerprint of
// the plan the user was shown: the server deletes nothing if the plan has
// changed since. Confirmed, also sent with CleanupExecute only, is how the
// removal was confirmed: CleanupConfirmedTyped or CleanupConfirmedFlag.
type CleanupRequest struct {
	Mode      string
	Tables    []string
	Fields    []string
	Expect    string
	Confirmed string
}

// CleanupPlan is everything a cleanup deletes: what a describe says it would
// delete, and what an execute says it did. A blocked plan cannot be executed.
type CleanupPlan struct {
	AppID       string        `json:"app_id"`
	Fingerprint string        `json:"fingerprint"`
	Blocked     bool          `json:"blocked"`
	Items       []CleanupItem `json:"items"`
}

// CleanupItem is one table or field of a plan. Kind is "table" or "field",
// and Name is "table" or "table.field". Rows is the table's row count, or for
// a field the number of rows holding a value. InDatabase is false for an item
// that only has leftover metadata. Metadata holds the non-zero counts of the
// records that go with the item, in the order to show them. OtherApps holds
// what the removal also takes from other applications, and is left out when
// it takes nothing.
type CleanupItem struct {
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	InDatabase bool              `json:"in_database"`
	Rows       int64             `json:"rows"`
	Metadata   []CleanupMetadata `json:"metadata"`
	OtherApps  []CleanupOtherApp `json:"other_apps,omitempty"`
	Blockers   []string          `json:"blockers"`
}

// CleanupMetadata counts one kind of record that goes with a CleanupItem.
// What is the server's label for it, such as "fields".
type CleanupMetadata struct {
	What  string `json:"what"`
	Count int64  `json:"count"`
}

// CleanupOtherApp counts one kind of record of another application that a
// CleanupItem takes with it. What is the server's label for it, such as
// "db events".
type CleanupOtherApp struct {
	AppID string `json:"app_id"`
	What  string `json:"what"`
	Count int64  `json:"count"`
}

// Cleanup sends one cleanup request and returns the plan the server answers
// with.
func (c *Client) Cleanup(ctx context.Context, req CleanupRequest) (*CleanupPlan, error) {
	if c.channel == nil {
		return nil, fmt.Errorf("not joined to channel")
	}

	payload := map[string]any{
		"mode":   req.Mode,
		"tables": listOrEmpty(req.Tables),
		"fields": listOrEmpty(req.Fields),
	}
	if req.Mode == CleanupExecute {
		payload["expect"] = req.Expect
		payload["confirmed"] = req.Confirmed
	}

	reply, err := c.channel.Request(ctx, "cleanup", payload, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("cleanup: %w", err)
	}

	response, _ := reply.Response.(map[string]any)
	if reply.Status != "ok" {
		msg := "cleanup failed"
		if m, ok := response["message"].(string); ok {
			msg = m
		}
		return nil, errors.New(msg)
	}

	if response["plan"] == nil {
		return nil, errors.New("cleanup: the reply has no plan")
	}
	// The reply is JSON the socket already decoded, so encoding it cannot fail.
	encoded, _ := json.Marshal(response["plan"])
	plan := &CleanupPlan{}
	if err := json.Unmarshal(encoded, plan); err != nil {
		return nil, fmt.Errorf("cleanup: unreadable plan: %w", err)
	}
	return plan, nil
}

// listOrEmpty returns list, or an empty list for nil, so a payload carries []
// and never null.
func listOrEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
