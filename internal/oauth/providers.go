package oauth

import (
	"encoding/json"
	"fmt"
)

// NewTwitch builds the Twitch OAuth provider. Register redirectURL as an
// "OAuth Redirect URL" on your app at https://dev.twitch.tv/console/apps.
func NewTwitch(clientID, clientSecret, redirectURL string) *Provider {
	return &Provider{
		Name:         "twitch",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      "https://id.twitch.tv/oauth2/authorize",
		TokenURL:     "https://id.twitch.tv/oauth2/token",
		UserInfoURL:  "https://api.twitch.tv/helix/users",
		// channel:read:subscriptions/bits:read let internal/platform/twitch
		// subscribe to this user's own channel's EventSub subscribe/gift-sub
		// and cheer events once they've authorized the app — confirmed
		// (empirically, against the live API) to require the channel's own
		// broadcaster specifically; a moderator granting these does not
		// let an app-token subscription succeed for a channel they don't
		// broadcast. user:read:moderated_channels is requested anyway
		// (see twitch.Client.ModeratedChannels) since it still lists which
		// channels a moderator could try, even though that alone doesn't
		// unlock EventSub for them.
		//
		// user:read:chat/user:bot back reading a channel's chat for
		// "!timer pause"/"!timer unpause"/"!timer lock"/"!timer unlock"/
		// "!timer hide"/"!timer unhide" commands (see
		// twitch.Client.EnsureChatCommandSubscription) — unlike the scopes
		// above, Twitch does honor moderator status for this one (or
		// broadcaster status on one's own channel), no broadcaster-only
		// restriction.
		//
		// Accounts that linked Twitch before any of these were added here
		// won't have granted them; the affected calls fail (logged, not
		// fatal) until they reconnect their Twitch account.
		Scopes: []string{
			"channel:read:subscriptions", "bits:read", "user:read:moderated_channels",
			"user:read:chat", "user:bot",
		},
		ExtraUserInfoHeaders: map[string]string{"Client-Id": clientID},
		ParseUser:            parseTwitchUser,
	}
}

func parseTwitchUser(body []byte) (platformUserID, username string, err error) {
	var resp struct {
		Data []struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("parse twitch user response: %w", err)
	}
	if len(resp.Data) == 0 {
		return "", "", fmt.Errorf("twitch user response had no data")
	}
	return resp.Data[0].ID, resp.Data[0].Login, nil
}

// NewKick builds the Kick OAuth provider.
//
// NOTE: Kick's public OAuth API is comparatively new. AuthURL, TokenURL,
// UserInfoURL, and parseKickUser's response shape below are this
// integration's best-known values, not verified against a live
// application — check https://docs.kick.com before relying on this in
// production, and adjust parseKickUser if the user-info response shape
// differs. PKCE is required by Kick's API.
func NewKick(clientID, clientSecret, redirectURL string) *Provider {
	return &Provider{
		Name:         "kick",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      "https://id.kick.com/oauth/authorize",
		TokenURL:     "https://id.kick.com/oauth/token",
		UserInfoURL:  "https://api.kick.com/public/v1/users",
		// events:subscribe is required for internal/platform/kick to
		// subscribe to this broadcaster's sub/gift-sub webhook events once
		// they've authorized the app — like the rest of this Kick
		// integration, this scope name is this project's best-known value,
		// not verified against a live application.
		Scopes:    []string{"user:read", "events:subscribe"},
		UsePKCE:   true,
		ParseUser: parseKickUser,
	}
}

func parseKickUser(body []byte) (platformUserID, username string, err error) {
	var resp struct {
		Data []struct {
			UserID int    `json:"user_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("parse kick user response: %w", err)
	}
	if len(resp.Data) == 0 {
		return "", "", fmt.Errorf("kick user response had no data")
	}
	return fmt.Sprintf("%d", resp.Data[0].UserID), resp.Data[0].Name, nil
}
