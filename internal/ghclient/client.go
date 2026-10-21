package ghclient

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Repo           string
	Token          string
	AppID          int64
	InstallationID int64
	PrivateKey     []byte
	BaseURL        string
	BaseBranch     string
}

type Client struct {
	repo              string
	token             string
	appID             int64
	installationID    int64
	privateKey        *rsa.PrivateKey
	baseURL           string
	baseBranch        string
	httpClient        *http.Client
	tokenMu           sync.Mutex
	installationToken string
	tokenExpires      time.Time
}

type PullRequestInput struct {
	Path    string
	Content []byte
	Title   string
	Body    string
}

type CreatedPullRequest struct {
	URL    string
	Number int
	Branch string
}

type PullFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github api status %d: %s", e.Status, e.Body)
}

func New(cfg Config) (*Client, error) {
	repo := strings.Trim(strings.TrimSpace(cfg.Repo), "/")
	if strings.Count(repo, "/") != 1 {
		return nil, errors.New("github.repo must be owner/repo")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	baseBranch := strings.TrimSpace(cfg.BaseBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}

	client := &Client{
		repo:           repo,
		token:          strings.TrimSpace(cfg.Token),
		appID:          cfg.AppID,
		installationID: cfg.InstallationID,
		baseURL:        baseURL,
		baseBranch:     baseBranch,
		httpClient:     &http.Client{Timeout: 20 * time.Second},
	}
	useApp := cfg.AppID > 0 || cfg.InstallationID > 0 || len(cfg.PrivateKey) > 0
	if useApp {
		if cfg.AppID <= 0 || cfg.InstallationID <= 0 || len(cfg.PrivateKey) == 0 {
			return nil, errors.New("github app auth requires app id, installation id and private key")
		}
		key, err := parsePrivateKey(cfg.PrivateKey)
		if err != nil {
			return nil, err
		}
		client.privateKey = key
		client.token = ""
	} else if client.token == "" {
		return nil, errors.New("github.token or github app credentials are required")
	}
	return client, nil
}

func (c *Client) Repo() string {
	return c.repo
}

func (c *Client) BaseBranch() string {
	return c.baseBranch
}

func (c *Client) HistoryURL(path string) string {
	path = strings.TrimPrefix(path, "/")
	return fmt.Sprintf("https://github.com/%s/commits/%s/%s", c.repo, escapePath(c.baseBranch), escapePath(path))
}

func (c *Client) CreatePullRequest(ctx context.Context, input PullRequestInput) (*CreatedPullRequest, error) {
	path, err := cleanRepoPath(input.Path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Title) == "" {
		return nil, errors.New("pull request title is required")
	}

	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+c.repo+"/git/ref/heads/"+escapePath(c.baseBranch), nil, &ref); err != nil {
		return nil, err
	}
	if ref.Object.SHA == "" {
		return nil, errors.New("base branch sha is empty")
	}

	suffix, err := randomHex(4)
	if err != nil {
		return nil, err
	}
	branch := fmt.Sprintf("github.com/EggyFly/Wiki-X/%d-%s", time.Now().Unix(), suffix)
	if err := c.do(ctx, http.MethodPost, "/repos/"+c.repo+"/git/refs", map[string]string{
		"ref": "refs/heads/" + branch,
		"sha": ref.Object.SHA,
	}, nil); err != nil {
		return nil, err
	}

	var existing struct {
		SHA string `json:"sha"`
	}
	err = c.do(ctx, http.MethodGet, "/repos/"+c.repo+"/contents/"+escapePath(path)+"?ref="+url.QueryEscape(branch), nil, &existing)
	hasExisting := err == nil
	if err != nil {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
			return nil, err
		}
	}

	payload := map[string]string{
		"message": "Update " + path + " via Wiki-X",
		"content": base64.StdEncoding.EncodeToString(input.Content),
		"branch":  branch,
	}
	if hasExisting {
		payload["sha"] = existing.SHA
	}
	if err := c.do(ctx, http.MethodPut, "/repos/"+c.repo+"/contents/"+escapePath(path), payload, nil); err != nil {
		return nil, err
	}

	var pull struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := c.do(ctx, http.MethodPost, "/repos/"+c.repo+"/pulls", map[string]string{
		"title": input.Title,
		"head":  branch,
		"base":  c.baseBranch,
		"body":  input.Body,
	}, &pull); err != nil {
		return nil, err
	}
	return &CreatedPullRequest{URL: pull.HTMLURL, Number: pull.Number, Branch: branch}, nil
}

func (c *Client) PullRequestFiles(ctx context.Context, number int) ([]PullFile, error) {
	var all []PullFile
	for page := 1; page <= 10; page++ {
		var files []PullFile
		path := fmt.Sprintf("/repos/%s/pulls/%d/files?per_page=100&page=%d", c.repo, number, page)
		if err := c.do(ctx, http.MethodGet, path, nil, &files); err != nil {
			return nil, err
		}
		all = append(all, files...)
		if len(files) < 100 {
			break
		}
	}
	return all, nil
}

func (c *Client) FileContent(ctx context.Context, path, ref string) ([]byte, error) {
	clean, err := cleanRepoPath(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Size     int64  `json:"size"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+c.repo+"/contents/"+escapePath(clean)+"?ref="+url.QueryEscape(ref), nil, &file); err != nil {
		return nil, err
	}
	if file.Type != "file" || file.Encoding != "base64" {
		return nil, errors.New("github file is not a base64 file")
	}
	if file.Size > 1<<20 {
		return nil, errors.New("github file is too large")
	}
	data, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) FileBlob(ctx context.Context, path, ref string, maxSize int64) ([]byte, error) {
	clean, err := cleanRepoPath(path)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+c.repo+"/contents/"+escapePath(clean)+"?ref="+url.QueryEscape(ref), nil, &meta); err != nil {
		return nil, err
	}
	if meta.Type != "file" || meta.SHA == "" {
		return nil, errors.New("github path is not a file")
	}
	if maxSize > 0 && meta.Size > maxSize {
		return nil, errors.New("github file is too large")
	}
	var blob struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Size     int64  `json:"size"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+c.repo+"/git/blobs/"+url.PathEscape(meta.SHA), nil, &blob); err != nil {
		return nil, err
	}
	if blob.Encoding != "base64" {
		return nil, errors.New("github blob is not base64")
	}
	if maxSize > 0 && blob.Size > maxSize {
		return nil, errors.New("github blob is too large")
	}
	data, err := base64.StdEncoding.DecodeString(blob.Content)
	if err != nil {
		return nil, err
	}
	if maxSize > 0 && int64(len(data)) > maxSize {
		return nil, errors.New("github blob is too large")
	}
	return data, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	token, err := c.authToken(ctx)
	if err != nil {
		return err
	}
	return c.doWithToken(ctx, token, method, path, body, out)
}

func (c *Client) doWithToken(ctx context.Context, token, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Wiki-X")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
	}
	return nil
}

func (c *Client) authToken(ctx context.Context) (string, error) {
	if c.token != "" {
		return c.token, nil
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.installationToken != "" && time.Now().Before(c.tokenExpires.Add(-time.Minute)) {
		return c.installationToken, nil
	}
	jwt, err := c.appJWT()
	if err != nil {
		return "", err
	}
	var response struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := "/app/installations/" + strconv.FormatInt(c.installationID, 10) + "/access_tokens"
	if err := c.doWithToken(ctx, jwt, http.MethodPost, path, nil, &response); err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", errors.New("github app installation token is empty")
	}
	c.installationToken = response.Token
	c.tokenExpires = response.ExpiresAt
	return response.Token, nil
}

func (c *Client) appJWT() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":%d}`, now.Add(-time.Minute).Unix(), now.Add(9*time.Minute).Unix(), c.appID)
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	input := header + "." + payload
	sum := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("github private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	value, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github private key: %w", err)
	}
	key, ok := value.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github private key is not RSA")
	}
	return key, nil
}

func cleanRepoPath(path string) (string, error) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	if path == "" || strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return "", errors.New("invalid github path")
	}
	return path, nil
}

func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
