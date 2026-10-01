// Package trello is the adapter for the Trello REST API: record shapes, endpoints, and pagination.
//
// Field names come from Trello's REST documentation. They have not yet been confirmed against a
// live response; see the TODO list in the implementation report.
package trello

import "encoding/json"

// These types exist to derive describe's outputSchema. Records themselves pass through as ordered
// JSON, so fields Trello adds later are not lost, and --fields decides what is shown.

type Board struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Desc             string `json:"desc,omitempty"`
	Closed           bool   `json:"closed,omitempty"`
	IDOrganization   string `json:"idOrganization,omitempty"`
	Pinned           bool   `json:"pinned,omitempty"`
	Starred          bool   `json:"starred,omitempty"`
	URL              string `json:"url,omitempty"`
	ShortURL         string `json:"shortUrl,omitempty"`
	DateLastActivity string `json:"dateLastActivity,omitempty"`
}

type List struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Closed     bool        `json:"closed,omitempty"`
	Pos        json.Number `json:"pos,omitempty"`
	IDBoard    string      `json:"idBoard,omitempty"`
	Subscribed bool        `json:"subscribed,omitempty"`
}

type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}

type Card struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Desc             string          `json:"desc,omitempty"`
	Closed           bool            `json:"closed,omitempty"`
	IDList           string          `json:"idList,omitempty"`
	IDBoard          string          `json:"idBoard,omitempty"`
	IDMembers        []string        `json:"idMembers,omitempty"`
	IDLabels         []string        `json:"idLabels,omitempty"`
	Labels           []Label         `json:"labels,omitempty"`
	Due              string          `json:"due,omitempty"`
	DueComplete      bool            `json:"dueComplete,omitempty"`
	Start            string          `json:"start,omitempty"`
	DateLastActivity string          `json:"dateLastActivity,omitempty"`
	Pos              json.Number     `json:"pos,omitempty"`
	URL              string          `json:"url,omitempty"`
	ShortURL         string          `json:"shortUrl,omitempty"`
	ShortLink        string          `json:"shortLink,omitempty"`
	Badges           json.RawMessage `json:"badges,omitempty"`
}

type MemberRef struct {
	ID       string `json:"id"`
	Username string `json:"username,omitempty"`
	FullName string `json:"fullName,omitempty"`
}

type Action struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Date            string          `json:"date"`
	IDMemberCreator string          `json:"idMemberCreator,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
	MemberCreator   *MemberRef      `json:"memberCreator,omitempty"`
}

type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	URL      string `json:"url,omitempty"`
	Bytes    *int64 `json:"bytes,omitempty"`
	Date     string `json:"date,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Field sets for describe and --fields. Defaults are the handful an agent needs to decide what to
// look at next; bulky fields are available but not default.
var (
	BoardDefault   = []string{"id", "name", "closed", "dateLastActivity", "url"}
	BoardAvailable = []string{"id", "name", "closed", "dateLastActivity", "url", "desc", "shortUrl", "idOrganization", "starred", "pinned"}

	ListDefault   = []string{"id", "name", "closed", "pos"}
	ListAvailable = []string{"id", "name", "closed", "pos", "idBoard", "subscribed"}

	CardDefault   = []string{"id", "name", "idList", "due", "dateLastActivity"}
	CardAvailable = []string{"id", "name", "idList", "due", "dateLastActivity", "desc", "closed", "idBoard", "idMembers",
		"idLabels", "labels", "dueComplete", "start", "pos", "shortUrl", "url", "shortLink", "badges.*"}
	CardGetDefault = []string{"id", "name", "desc", "idList", "idBoard", "due", "dueComplete", "labels", "idMembers", "dateLastActivity", "url"}

	ActionDefault   = []string{"id", "type", "date", "idMemberCreator"}
	ActionAvailable = []string{"id", "type", "date", "idMemberCreator", "data.*", "memberCreator.*"}

	AttachmentDefault   = []string{"id", "name", "url", "bytes", "date", "mimeType"}
	AttachmentAvailable = []string{"id", "name", "url", "bytes", "date", "mimeType"}
)
