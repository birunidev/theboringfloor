package spaceboard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/board"
)

// Client is a minimal MCP (streamable HTTP, JSON-RPC) client for SpaceURL.
type Client struct {
	endpoint string // SpaceURL in production; a test server in tests
	tokens   interface {
		Token(context.Context) (string, error)
		Invalidate()
	}
	http *http.Client

	mu      sync.Mutex
	session string
	inited  bool
	nextID  int64
}

// New returns a client for SpaceURL using ts for bearer tokens.
func New(ts *TokenSource) *Client {
	return &Client{endpoint: SpaceURL, tokens: ts, http: &http.Client{Timeout: 60 * time.Second}}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *Client) post(ctx context.Context, payload map[string]any, wantID bool) (*rpcResponse, int, error) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, 0, err
	}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+tok)
	c.mu.Lock()
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	c.mu.Unlock()
	res, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if sid := res.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.session = sid
		c.mu.Unlock()
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusNotFound {
		return nil, res.StatusCode, fmt.Errorf("space mcp: http %d", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, res.StatusCode, fmt.Errorf("space mcp: http %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	if !wantID {
		return nil, res.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		return parseSSE(body, payload["id"])
	}
	var r rpcResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, res.StatusCode, fmt.Errorf("space mcp: bad response: %w", err)
	}
	return &r, res.StatusCode, nil
}

func parseSSE(body []byte, id any) (*rpcResponse, int, error) {
	want, _ := json.Marshal(id)
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var data strings.Builder
	flush := func() *rpcResponse {
		defer data.Reset()
		var r rpcResponse
		if data.Len() > 0 && json.Unmarshal([]byte(data.String()), &r) == nil && bytes.Equal(bytes.TrimSpace(r.ID), want) {
			return &r
		}
		return nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "":
			if r := flush(); r != nil {
				return r, 200, nil
			}
		}
	}
	if r := flush(); r != nil {
		return r, 200, nil
	}
	return nil, 200, errors.New("space mcp: no response in event stream")
}

func (c *Client) id() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return c.nextID
}

func (c *Client) initialize(ctx context.Context) (int, error) {
	r, status, err := c.post(ctx, map[string]any{
		"jsonrpc": "2.0", "id": c.id(), "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "theboringfloor-autopilot", "version": "0.1"},
		},
	}, true)
	if err != nil {
		return status, err
	}
	if r.Error != nil {
		return status, fmt.Errorf("space mcp initialize: %s", r.Error.Message)
	}
	if _, st, err := c.post(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, false); err != nil {
		return st, err
	}
	c.mu.Lock()
	c.inited = true
	c.mu.Unlock()
	return status, nil
}

// CallTool calls one Space tool and decodes its JSON result into out.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		c.mu.Lock()
		inited := c.inited
		c.mu.Unlock()
		if !inited {
			status, err := c.initialize(ctx)
			if status == http.StatusUnauthorized && attempt == 0 {
				c.tokens.Invalidate()
				continue
			}
			if err != nil {
				return err
			}
		}
		r, status, err := c.post(ctx, map[string]any{
			"jsonrpc": "2.0", "id": c.id(), "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": args},
		}, true)
		if status == http.StatusUnauthorized || status == http.StatusNotFound {
			// Expired token or session: refresh / re-initialize once.
			if status == http.StatusUnauthorized {
				c.tokens.Invalidate()
			}
			c.mu.Lock()
			c.inited, c.session = false, ""
			c.mu.Unlock()
			continue
		}
		if err != nil {
			return err
		}
		if r.Error != nil {
			return fmt.Errorf("space %s: %s", name, r.Error.Message)
		}
		return decodeToolResult(name, r.Result, out)
	}
	return fmt.Errorf("space %s: still unauthorized after refresh", name)
}

func decodeToolResult(name string, raw json.RawMessage, out any) error {
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("space %s: bad tool result: %w", name, err)
	}
	text := ""
	for _, c := range res.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	if res.IsError {
		return fmt.Errorf("space %s: %s", name, trim(text, 300))
	}
	if out == nil {
		return nil
	}
	if len(res.Structured) > 0 && string(res.Structured) != "null" {
		return json.Unmarshal(res.Structured, out)
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("space %s: result is not JSON: %s", name, trim(text, 200))
	}
	return nil
}

// --- board.Board ---

type wireCard struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	BoardID     string   `json:"boardId"`
	ListID      string   `json:"listId"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	LabelIDs    []string `json:"labelIds"`
	UpdatedAt   string   `json:"updatedAt"`
	Comments    []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		At   string `json:"at"`
	} `json:"comments"`
}

func (w wireCard) card() board.Card {
	c := board.Card{ID: w.ID, Key: w.Key, BoardID: w.BoardID, ListID: w.ListID, Title: w.Title,
		Description: w.Description, LabelIDs: w.LabelIDs, UpdatedAt: w.UpdatedAt}
	for _, cm := range w.Comments {
		c.Comments = append(c.Comments, board.Comment{ID: cm.ID, Text: cm.Text, At: cm.At})
	}
	return c
}

// ListCards lists every card of a list (all pages).
func (c *Client) ListCards(ctx context.Context, boardID, listID string) ([]board.Card, error) {
	var out []board.Card
	cursor := ""
	for page := 0; page < 50; page++ {
		args := map[string]any{"board_id": boardID, "limit": 50}
		if listID != "" {
			args["list_id"] = listID
		}
		if cursor != "" {
			args["cursor"] = cursor
		}
		var res struct {
			Cards      []wireCard `json:"cards"`
			NextCursor string     `json:"nextCursor"`
			HasMore    bool       `json:"hasMore"`
		}
		if err := c.CallTool(ctx, "list_cards", args, &res); err != nil {
			return nil, err
		}
		for _, w := range res.Cards {
			out = append(out, w.card())
		}
		if !res.HasMore || res.NextCursor == "" {
			return out, nil
		}
		cursor = res.NextCursor
	}
	return out, errors.New("space list_cards: too many pages")
}

// GetCard reads one card with its comments.
func (c *Client) GetCard(ctx context.Context, cardID string) (board.Card, error) {
	var res struct {
		Card wireCard `json:"card"`
	}
	if err := c.CallTool(ctx, "get_card", map[string]any{"card_id": cardID}, &res); err != nil {
		return board.Card{}, err
	}
	return res.Card.card(), nil
}

// MoveCard moves a card. Always wrap the client in board.Guard.
func (c *Client) MoveCard(ctx context.Context, cardID, listID, idemKey string) error {
	return c.CallTool(ctx, "move_card", map[string]any{"card_id": cardID, "list_id": listID, "idempotency_key": idemKey}, nil)
}

// AddComment comments on a card.
func (c *Client) AddComment(ctx context.Context, cardID, text, idemKey string) error {
	return c.CallTool(ctx, "add_comment", map[string]any{"card_id": cardID, "text": text, "idempotency_key": idemKey}, nil)
}

// List is a board list.
type List struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Label is a board label.
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// BoardInfo is what get_board returns that the autopilot needs.
type BoardInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Lists       []List  `json:"lists"`
	Labels      []Label `json:"labels"`
}

// GetBoard reads a board's lists and labels.
func (c *Client) GetBoard(ctx context.Context, boardID string) (BoardInfo, error) {
	var res struct {
		Board  BoardInfo `json:"board"`
		Lists  []List    `json:"lists"`
		Labels []Label   `json:"labels"`
	}
	if err := c.CallTool(ctx, "get_board", map[string]any{"board_id": boardID}, &res); err != nil {
		return BoardInfo{}, err
	}
	b := res.Board
	if len(b.Lists) == 0 {
		b.Lists = res.Lists
	}
	if len(b.Labels) == 0 {
		b.Labels = res.Labels
	}
	return b, nil
}

// ListID finds a list by case-insensitive name.
func (b BoardInfo) ListID(name string) string {
	for _, l := range b.Lists {
		if strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(name)) {
			return l.ID
		}
	}
	return ""
}

// LabelID finds a label by case-insensitive name.
func (b BoardInfo) LabelID(name string) string {
	for _, l := range b.Labels {
		if strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(name)) {
			return l.ID
		}
	}
	return ""
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

var _ board.Board = (*Client)(nil)
