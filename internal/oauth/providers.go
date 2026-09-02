package oauth

import (
	"encoding/json"
	"fmt"
)

// NewTwitch builds the Twitch OAuth provider. Register redirectURL as an
// "OAuth Redirect URL" on your app at https://dev.twitch.tv/console/apps.
func NewTwitch(clientID, clientSecret, redirectURL string) *Provider {
	return &Provider{
		Name:                 "twitch",
		ClientID:             clientID,
		ClientSecret:         clientSecret,
		RedirectURL:          redirectURL,
		AuthURL:              "https://id.twitch.tv/oauth2/authorize",
		TokenURL:             "https://id.twitch.tv/oauth2/token",
		UserInfoURL:          "https://api.twitch.tv/helix/users",
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
		Scopes:       []string{"user:read"},
		UsePKCE:      true,
		ParseUser:    parseKickUser,
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
