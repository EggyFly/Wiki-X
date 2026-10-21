package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/EggyFly/Wiki-X/internal/config"
)

type githubOAuth struct {
	config *oauth2.Config
}

type GitHubProfile struct {
	Subject string
	Login   string
	Name    string
	Email   string
}

func newGitHubOAuth(cfg config.GitHubOAuthConfig) *githubOAuth {
	return &githubOAuth{config: &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       []string{"read:user", "user:email"},
		Endpoint:     github.Endpoint,
	}}
}

func (g *githubOAuth) AuthURL(state string) string {
	return g.config.AuthCodeURL(state)
}

func (g *githubOAuth) Exchange(ctx context.Context, code string) (*GitHubProfile, error) {
	token, err := g.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("github exchange: %w", err)
	}
	client := g.config.Client(ctx, token)
	client.Timeout = 10 * time.Second

	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := getJSON(ctx, client, "https://api.github.com/user", &user); err != nil {
		return nil, err
	}
	email := user.Email
	if email == "" {
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := getJSON(ctx, client, "https://api.github.com/user/emails", &emails); err == nil {
			for _, item := range emails {
				if item.Primary && item.Verified {
					email = item.Email
					break
				}
			}
		}
	}
	return &GitHubProfile{
		Subject: strconv.FormatInt(user.ID, 10),
		Login:   user.Login,
		Name:    user.Name,
		Email:   email,
	}, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Wiki-X")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("github api %s: %d %s", url, resp.StatusCode, string(body))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target)
}
