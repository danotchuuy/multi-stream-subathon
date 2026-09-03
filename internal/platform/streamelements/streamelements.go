// Package streamelements polls StreamElements' REST API for a channel's
// recent tips (donations), translating them into subathon.Event using a
// timer's reward/money rules — see Poller in poller.go. Unlike Twitch/Kick,
// there's no server-level app credential: each timer owner supplies their
// own account's JWT token (from
// streamelements.com/dashboard/account/channels), scoped to their own
// StreamElements channel, so every call here takes it as a parameter
// rather than the package holding one Config for the whole server.
//
// NOTE on why this polls instead of using StreamElements' real-time
// gateway (wss://realtime.streamelements.com, Socket.IO 2.x/Engine.IO 3
// framing): that would need a persistent outbound connection per
// configured timer, hand-rolling the socket.io wire protocol on top of
// this project's existing gorilla/websocket dependency (there's no
// socket.io client library here, and one wasn't worth adding for a single
// feature). Polling GET /activities/{channel}?types=tip every
// pollInterval trades a little latency for a much simpler, more robust
// implementation — the same trade-off donations already have generally,
// since nothing else feeds them live either (see the dashboard's "Add
// donation" form, which computes the same way at click-time instead of
// on a timer).
package streamelements

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// apiBase is a var (not const) so tests can point it at a local server.
var apiBase = "https://api.streamelements.com/kappa/v2"

// Client is a thin wrapper around StreamElements' REST API. It holds no
// per-account state itself — every call takes the caller's own JWT token
// (see package doc) — so one Client is shared across every timer.
type Client struct {
	hc *http.Client
}

// NewClient builds a Client ready to use; no configuration needed since
// StreamElements has no server-level app credential here.
func NewClient() *Client {
	return &Client{hc: &http.Client{Timeout: 15 * time.Second}}
}

// Account is the channel identity a JWT token resolves to (see
// ResolveChannel) — enough to confirm to a timer owner which
// StreamElements account they just connected, and to address RecentTips.
type Account struct {
	ChannelID   string
	DisplayName string
}

// ResolveChannel validates token and returns the StreamElements channel it
// belongs to, via GET /channels/me. Also serves as the save-time
// validation for a newly entered token: an invalid/expired one fails here
// with a clear error instead of silently never producing any tips.
func (c *Client) ResolveChannel(token string) (Account, error) {
	var resp struct {
		ID          string `json:"_id"`
		DisplayName string `json:"displayName"`
		Username    string `json:"username"`
	}
	if err := c.get(token, apiBase+"/channels/me", &resp); err != nil {
		return Account{}, err
	}
	if resp.ID == "" {
		return Account{}, fmt.Errorf("streamelements: channels/me returned no channel id")
	}
	name := resp.DisplayName
	if name == "" {
		name = resp.Username
	}
	return Account{ChannelID: resp.ID, DisplayName: name}, nil
}

// Tip is one donation activity, as returned by RecentTips.
type Tip struct {
	ID        string
	Username  string
	Message   string
	Amount    float64
	Currency  string
	CreatedAt time.Time
}

// RecentTips returns every tip activity for channelID strictly after
// since, oldest first, capped at limit (StreamElements accepts 1-500 per
// call — see api.streamelements.com/kappa/v2/activities/{channel}).
func (c *Client) RecentTips(token, channelID string, since time.Time, limit int) ([]Tip, error) {
	q := url.Values{
		"types":  {"tip"},
		"after":  {strconv.FormatInt(since.UnixMilli()+1, 10)},
		"before": {strconv.FormatInt(time.Now().UnixMilli(), 10)},
		"limit":  {strconv.Itoa(limit)},
	}
	endpoint := fmt.Sprintf("%s/activities/%s?%s", apiBase, url.PathEscape(channelID), q.Encode())

	var resp []struct {
		ID   string `json:"_id"`
		Type string `json:"type"`
		Data struct {
			Username string  `json:"username"`
			Message  string  `json:"message"`
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
		} `json:"data"`
		CreatedAt time.Time `json:"createdAt"`
	}
	if err := c.get(token, endpoint, &resp); err != nil {
		return nil, err
	}

	tips := make([]Tip, 0, len(resp))
	for _, a := range resp {
		// The types=tip query parameter should already filter server-side;
		// this is a defensive check in case that's ever not honored.
		if a.Type != "tip" {
			continue
		}
		tips = append(tips, Tip{
			ID:        a.ID,
			Username:  a.Data.Username,
			Message:   a.Data.Message,
			Amount:    a.Data.Amount,
			Currency:  a.Data.Currency,
			CreatedAt: a.CreatedAt,
		})
	}
	sort.Slice(tips, func(i, j int) bool { return tips[i].CreatedAt.Before(tips[j].CreatedAt) })
	return tips, nil
}

func (c *Client) get(token, endpoint string, out any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("streamelements API %s: %s: %s", endpoint, resp.Status, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
