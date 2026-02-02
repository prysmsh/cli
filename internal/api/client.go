package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/prysmsh/pkg/tlsutil"
)

// Client wraps HTTP access to the Prysm control plane API.
type Client struct {
	baseURL            *url.URL
	httpClient         *http.Client
	userAgent          string
	debug              bool
	hostOverride       string
	insecureSkipVerify bool
	dialOverride       string

	mu    sync.RWMutex
	token string
}

// Option mutates client configuration.
type Option func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithTimeout sets the HTTP timeout on the underlying client.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if c.httpClient == nil {
			c.httpClient = &http.Client{}
		}
		c.httpClient.Timeout = timeout
	}
}

// WithUserAgent configures a custom user agent.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// WithDebug toggles debug logging.
func WithDebug(debug bool) Option {
	return func(c *Client) {
		c.debug = debug
	}
}

// WithHostOverride sets a custom Host header on outgoing requests.
func WithHostOverride(host string) Option {
	return func(c *Client) {
		c.hostOverride = strings.TrimSpace(host)
	}
}

// WithInsecureSkipVerify toggles TLS certificate verification.
func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Client) {
		c.insecureSkipVerify = skip
	}
}

// WithDialAddress overrides the network address used when dialing the API host.
func WithDialAddress(addr string) Option {
	return func(c *Client) {
		c.dialOverride = strings.TrimSpace(addr)
	}
}

// NewClient constructs a new API client.
func NewClient(base string, opts ...Option) *Client {
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + strings.TrimLeft(base, "/")
	}
	base = strings.TrimSuffix(base, "/")

	parsed, err := url.Parse(base)
	if err != nil {
		panic(fmt.Sprintf("invalid api base url: %s", err))
	}

	normalizedPath := strings.TrimSpace(parsed.Path)
	normalizedPath = strings.TrimSuffix(normalizedPath, "/")
	switch normalizedPath {
	case "", "/":
		parsed.Path = "/api/v1"
	case "/v1":
		parsed.Path = "/api/v1"
	default:
		if strings.EqualFold(normalizedPath, "/api") {
			parsed.Path = "/api/v1"
		}
	}

	client := &Client{
		baseURL:    parsed,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		userAgent:  "prysm-cli",
	}

	for _, opt := range opts {
		opt(client)
	}

	// Configure HTTP transport with optional TLS/dial overrides.
	baseTransport := &http.Transport{
		ForceAttemptHTTP2: false,
	}

	serverName := parsed.Hostname()
	if client.hostOverride != "" {
		serverName = client.hostOverride
	}

	if client.insecureSkipVerify {
		baseTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         serverName,
			NextProtos:         []string{"http/1.1"},
		}
	} else {
		baseTransport.TLSClientConfig = &tls.Config{
			ServerName: serverName,
			NextProtos: []string{"http/1.1"},
		}
	}
	tlsutil.ApplyPQCConfig(baseTransport.TLSClientConfig)

	if client.dialOverride != "" {
		dialAddr := client.dialOverride
		baseHost := parsed.Host
		if !strings.Contains(baseHost, ":") {
			if parsed.Scheme == "https" {
				baseHost += ":443"
			} else {
				baseHost += ":80"
			}
		}
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		baseTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.EqualFold(addr, baseHost) {
				return dialer.DialContext(ctx, network, dialAddr)
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}

	client.httpClient.Transport = baseTransport

	return client
}

// SetToken configures the bearer token for subsequent requests.
func (c *Client) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

// Token returns the current bearer token (e.g. for embedding in kubeconfig).
func (c *Client) Token() string {
	return c.getToken()
}

// BasePublicURL returns the API base URL (scheme + host) so the backend can put it in kubeconfig (proxy URL).
func (c *Client) BasePublicURL() string {
	if c.baseURL == nil {
		return ""
	}
	return c.baseURL.Scheme + "://" + c.baseURL.Host
}

// Do issues an HTTP request against the API and decodes the response into v when provided.
func (c *Client) Do(ctx context.Context, method, endpoint string, payload interface{}, v interface{}) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, endpoint, payload)
	if err != nil {
		return nil, err
	}

	if c.debug {
		fmt.Fprintf(os.Stderr, "[debug] %s %s\n", method, req.URL.String())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.debug {
			fmt.Fprintf(os.Stderr, "[debug] Request failed: %v\n", err)
		}
		// Check if this was a context cancellation or timeout
		if ctx.Err() != nil {
			return nil, fmt.Errorf("request cancelled or timed out: %w", ctx.Err())
		}
		return nil, fmt.Errorf("perform request: %w", err)
	}

	if c.debug {
		fmt.Fprintf(os.Stderr, "[debug] Response status: %s\n", resp.Status)
	}

	defer func() {
		if resp.Body != nil && v == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()

	if resp.StatusCode >= 400 {
		apiErr := parseAPIError(resp)
		return resp, apiErr
	}

	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return resp, fmt.Errorf("decode response: %w", err)
		}
	}

	return resp, nil
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, payload interface{}) (*http.Request, error) {
	method = strings.ToUpper(method)

	endpoint = strings.TrimSpace(endpoint)
	var rawQuery string
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		rawQuery = endpoint[idx+1:]
		endpoint = endpoint[:idx]
	}

	joinedPath := path.Join(c.baseURL.Path, strings.TrimLeft(endpoint, "/"))
	target := *c.baseURL
	target.Path = joinedPath
	if rawQuery != "" {
		target.RawQuery = rawQuery
	}

	var body io.ReadWriter
	if payload != nil {
		body = &bytes.Buffer{}
		if err := json.NewEncoder(body).Encode(payload); err != nil {
			return nil, fmt.Errorf("encode payload: %w", err)
		}
	}

	var reader io.Reader
	if body != nil {
		reader = body
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	if c.hostOverride != "" {
		req.Host = c.hostOverride
		// Some http clients prefer explicit Host header for non-default overrides.
		req.Header.Set("Host", c.hostOverride)
	}

	if token := c.getToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req, nil
}

func (c *Client) getToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// buildURL merges the base URL with the provided path segments.
func (c *Client) buildURL(elem ...string) string {
	segments := append([]string{c.baseURL.Path}, elem...)
	copied := *c.baseURL
	copied.Path = path.Join(segments...)
	return copied.String()
}
