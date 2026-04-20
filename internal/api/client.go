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

	// Use public DNS (1.1.1.1/8.8.8.8) via Go's pure-Go resolver to avoid
	// Tailscale MagicDNS or other VPN DNS blocking external domain lookups.
	// Requires GODEBUG=netdns=go (set in main.go init) to bypass cgo resolver.
	publicDNS := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", "1.1.1.1:53")
			if err != nil {
				conn, err = d.DialContext(ctx, "tcp", "8.8.8.8:53")
			}
			return conn, err
		},
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Resolver: publicDNS}

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
		baseTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.EqualFold(addr, baseHost) {
				return dialer.DialContext(ctx, network, dialAddr)
			}
			return dialer.DialContext(ctx, network, addr)
		}
	} else {
		baseTransport.DialContext = dialer.DialContext
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
		if resp.Body != nil {
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

// DoRaw performs an HTTP request with a raw body (e.g. for binary uploads).
// contentType should be the MIME type (e.g. "application/wasm").
func (c *Client) DoRaw(ctx context.Context, method, endpoint, contentType string, body io.Reader, v interface{}) (*http.Response, error) {
	endpoint = strings.TrimSpace(endpoint)
	joinedPath := path.Join(c.baseURL.Path, strings.TrimLeft(endpoint, "/"))
	target := *c.baseURL
	target.Path = joinedPath

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), target.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer func() {
		if resp.Body != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()

	if resp.StatusCode >= 400 {
		return resp, parseAPIError(resp)
	}
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return resp, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp, nil
}

// DoStream performs an HTTP request and returns the raw response w/o decoding.
// It leaves resp.Body open so callers can stream the payload themselves.
func (c *Client) DoStream(ctx context.Context, method, endpoint string, headers http.Header, body io.Reader) (*http.Response, error) {
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

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), target.String(), body)
	if err != nil {
		return nil, err
	}

	if headers != nil {
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
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

	// Do not send the access token for refresh; the backend uses the refresh_token in the body.
	if token := c.getToken(); token != "" {
		pathPart := strings.TrimLeft(endpoint, "/")
		if pathPart != "auth/refresh" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
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

// GetProxyResponse performs a GET to the cluster proxy (e.g. for discovery) and returns status code and body.
// Used to surface the backend's error message when kubectl gets a generic 503.
func (c *Client) GetProxyResponse(ctx context.Context, clusterID string) (statusCode int, body []byte, err error) {
	req, err := c.newRequest(ctx, "GET", "clusters/"+clusterID+"/proxy/api", nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}
