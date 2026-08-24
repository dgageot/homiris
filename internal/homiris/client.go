// Package homiris is a read-only client for the Homiris (EPS) smartphone API.
package homiris

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// AppVersion is the Homiris app version advertised to the API.
	AppVersion = "4.35.3"
	// DefaultBaseURL is the production API endpoint.
	DefaultBaseURL = "https://y41hsspp-mobile.eps-api.com/"

	apiPrefix = "smartphone/production/1.0.0/"

	// maxResponseSize caps what a homepage answer is allowed to allocate.
	maxResponseSize = 8 << 20
)

// Config holds the credentials needed to talk to the API.
type Config struct {
	BaseURL     string
	Login       string
	Password    string
	ClientToken string
	PhoneType   string
	HTTPClient  *http.Client
}

// Client fetches read-only data from the Homiris API. It caches the access
// token and the session and renews them when the API rejects them.
type Client struct {
	config Config

	mu          sync.Mutex
	accessToken string
	sessionID   string
}

// New returns a client using cfg, falling back to defaults for empty fields.
func New(config Config) *Client {
	config.BaseURL = cmp.Or(config.BaseURL, DefaultBaseURL)
	if config.HTTPClient == nil {
		config.HTTPClient = NewHTTPClient()
	}
	return &Client{config: config}
}

// NewHTTPClient returns the HTTP client used by default: short timeout and no
// redirect following, so a request is never silently replayed elsewhere.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Homepage returns everything the API reports about the system: alarm state,
// peripherals, last events... Fields are left untyped because the payload
// varies with the installed hardware.
func (c *Client) Homepage(ctx context.Context) (Homepage, error) {
	accessToken, sessionID, err := c.session(ctx)
	if err != nil {
		return nil, err
	}

	homepage, err := c.getHomepage(ctx, accessToken, sessionID)
	if err == nil || !isSessionExpired(err) {
		return homepage, err
	}

	// Sessions expire after a while: start a new one and try again.
	c.reset()
	accessToken, sessionID, err = c.session(ctx)
	if err != nil {
		return nil, err
	}
	return c.getHomepage(ctx, accessToken, sessionID)
}

func (c *Client) getHomepage(ctx context.Context, accessToken, sessionID string) (Homepage, error) {
	req, err := c.newAPIRequest(ctx, http.MethodGet, "homepage/"+url.PathEscape(sessionID), accessToken, nil)
	if err != nil {
		return nil, err
	}

	var homepage Homepage
	if err := c.do(req, &homepage); err != nil {
		return nil, fmt.Errorf("get homepage: %w", err)
	}
	return homepage, nil
}

func isSessionExpired(err error) bool {
	var apiError *APIError
	if !errors.As(err, &apiError) {
		return false
	}
	return apiError.StatusCode == http.StatusUnauthorized || apiError.StatusCode == http.StatusForbidden
}

func (c *Client) session(ctx context.Context) (accessToken, sessionID string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && c.sessionID != "" {
		return c.accessToken, c.sessionID, nil
	}

	accessToken, err = c.getAccessToken(ctx)
	if err != nil {
		return "", "", fmt.Errorf("get access token: %w", err)
	}
	sessionID, err = c.connect(ctx, accessToken)
	if err != nil {
		return "", "", fmt.Errorf("connect: %w", err)
	}

	c.accessToken, c.sessionID = accessToken, sessionID
	return accessToken, sessionID, nil
}

func (c *Client) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken, c.sessionID = "", ""
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {"PRODUCTION"},
	}
	req, err := c.newRequest(ctx, http.MethodPost, "token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+c.config.ClientToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.do(req, &response); err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", errors.New("response has no access token")
	}
	return response.AccessToken, nil
}

func (c *Client) connect(ctx context.Context, accessToken string) (string, error) {
	body := map[string]string{
		"application":   "SMARTPHONE",
		"typeDevice":    "SMARTPHONE",
		"pwd":           c.config.Password,
		"login":         c.config.Login,
		"originSession": "HOMIRIS",
		"phoneType":     c.config.PhoneType,
		"codeLanguage":  "FR",
		"version":       AppVersion,
		"timestamp":     "0",
		"system":        "",
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := c.newAPIRequest(ctx, http.MethodPost, "connect", accessToken, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}

	var response struct {
		SessionID string `json:"idSession"`
	}
	if err := c.do(req, &response); err != nil {
		return "", err
	}
	if response.SessionID == "" {
		return "", errors.New("response has no session ID")
	}
	return response.SessionID, nil
}

func (c *Client) newAPIRequest(ctx context.Context, method, path, accessToken string, body io.Reader) (*http.Request, error) {
	req, err := c.newRequest(ctx, method, apiPrefix+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Eps-Ctx-Source", "MOB-ABO")
	req.Header.Set("Eps-Ctx-Username", c.config.Login)
	return req, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	baseURL := strings.TrimRight(c.config.BaseURL, "/") + "/"
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Homiris/"+AppVersion)
	return req, nil
}

func (c *Client) do(req *http.Request, response any) error {
	res, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return newAPIError(res)
	}

	decoder := json.NewDecoder(io.LimitReader(res.Body, maxResponseSize))
	decoder.UseNumber()
	if err := decoder.Decode(response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// APIError reports a non-200 answer from the API.
type APIError struct {
	StatusCode int
	Status     string
	Body       string
}

func newAPIError(res *http.Response) *APIError {
	err := &APIError{StatusCode: res.StatusCode, Status: res.Status}
	if body, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<10)); readErr == nil {
		err.Body = strings.TrimSpace(string(body))
	}
	return err
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return "HTTP " + e.Status
	}
	return fmt.Sprintf("HTTP %s: %s", e.Status, e.Body)
}
