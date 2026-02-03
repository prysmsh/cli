package derp

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
	"github.com/prysmsh/pkg/tlsutil"
)

// EventType represents incoming DERP message categories.
type EventType string

const (
	EventPeerList         EventType = "peer_list"
	EventPeerJoined       EventType = "peer_joined"
	EventPeerLeft         EventType = "peer_left"
	EventRelayMessage     EventType = "relay_message"
	EventServiceDiscovery EventType = "service_discovery"
	EventStatsUpdate      EventType = "stats_update"
	EventPong             EventType = "pong"
	EventError            EventType = "error"
	EventRouteSetup       EventType = "route_setup"
	EventRouteResponse    EventType = "route_response"
	EventTrafficData      EventType = "traffic_data"
	EventUnknown          EventType = "unknown"
)

// TunnelTrafficHandler is called when tunnel traffic is received (route_setup or traffic_data).
// For route_setup: routeID, targetPort, externalPort are set; data is nil.
// For traffic_data: routeID and data are set.
type TunnelTrafficHandler func(routeID string, targetPort, externalPort int, data []byte)

// Client manages a DERP websocket connection.
type Client struct {
	url             string
	deviceID        string
	capabilities    map[string]interface{}
	headers         http.Header
	sessionToken    string
	derpTunnelToken string // Signed JWT with org binding; preferred over sessionToken

	dialer   *websocket.Dialer
	logLevel LogLevel
	logger   *log.Logger

	mu     sync.RWMutex
	conn   *websocket.Conn
	cancel context.CancelFunc

	// TunnelTrafficHandler is optional; when set, route_setup and traffic_data are forwarded.
	TunnelTrafficHandler TunnelTrafficHandler
}

// LogLevel controls verbosity.
type LogLevel int

const (
	// LogInfo emits informational events.
	LogInfo LogLevel = iota
	// LogDebug emits verbose events.
	LogDebug
)

// Option configures a DERP client instance.
type Option func(*Client)

// WithHeaders injects additional websocket headers.
func WithHeaders(h http.Header) Option {
	return func(c *Client) {
		c.headers = h.Clone()
	}
}

// WithCapabilities sets client capabilities advertised at registration.
func WithCapabilities(cap map[string]interface{}) Option {
	return func(c *Client) {
		c.capabilities = cap
	}
}

// WithLogLevel overrides logging verbosity.
func WithLogLevel(level LogLevel) Option {
	return func(c *Client) {
		c.logLevel = level
	}
}

// WithInsecure disables TLS certificate verification.
func WithInsecure(insecure bool) Option {
	return func(c *Client) {
		if insecure {
			c.dialer.TLSClientConfig.InsecureSkipVerify = true
		}
	}
}

// WithSessionToken sets the JWT session token for CLI registration.
func WithSessionToken(token string) Option {
	return func(c *Client) {
		c.sessionToken = token
	}
}

// WithDERPTunnelToken sets the signed DERP tunnel JWT (org binding cryptographically enforced).
// When set, this is preferred over session token for registration.
func WithDERPTunnelToken(token string) Option {
	return func(c *Client) {
		c.derpTunnelToken = token
	}
}

// WithTunnelTrafficHandler sets the callback for tunnel route_setup and traffic_data messages.
func WithTunnelTrafficHandler(h TunnelTrafficHandler) Option {
	return func(c *Client) {
		c.TunnelTrafficHandler = h
	}
}

// NewClient constructs a DERP websocket client.
func NewClient(url, deviceID string, opts ...Option) *Client {
	tlsConfig := &tls.Config{}
	tlsutil.ApplyPQCConfig(tlsConfig)
	client := &Client{
		url:      url,
		deviceID: deviceID,
		dialer: &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 10 * time.Second,
			TLSClientConfig:  tlsConfig,
		},
		logLevel: LogInfo,
		logger:   log.New(os.Stdout, "", 0),
		capabilities: map[string]interface{}{
			"platform":  "cli",
			"features":  []string{"service_discovery", "remote_commands"},
			"version":   "1.0.0",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// Run establishes the websocket connection and processes messages until context cancellation.
func (c *Client) Run(ctx context.Context) error {
	if c.deviceID == "" {
		return errors.New("device id is required")
	}

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	conn, _, err := c.dialer.DialContext(ctx, c.url, c.headers)
	if err != nil {
		return fmt.Errorf("connect to DERP: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	c.log(color.HiGreenString("Connected to DERP relay %s", c.url))

	if err := c.sendRegistration(); err != nil {
		return fmt.Errorf("send registration: %w", err)
	}

	pingTicker := time.NewTicker(30 * time.Second)
	heartbeatTicker := time.NewTicker(10 * time.Second)

	errCh := make(chan error, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
				var message map[string]interface{}
				if err := conn.ReadJSON(&message); err != nil {
					errCh <- fmt.Errorf("read DERP message: %w", err)
					return
				}
				c.handleMessage(message)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pingTicker.C:
				c.send(map[string]interface{}{"type": "ping"})
			case <-heartbeatTicker.C:
				c.send(map[string]interface{}{
					"type":      "heartbeat",
					"timestamp": time.Now().UTC().Format(time.RFC3339),
					"status":    "active",
				})
			}
		}
	}()

	defer func() {
		pingTicker.Stop()
		heartbeatTicker.Stop()
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Close terminates the websocket connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) sendRegistration() error {
	regPayload := map[string]interface{}{
		"device_id":     c.deviceID,
		"peer_type":     "client",
		"capabilities":  c.capabilities,
	}
	if c.derpTunnelToken != "" {
		regPayload["derp_tunnel_token"] = c.derpTunnelToken
	} else {
		regPayload["session_token"] = c.sessionToken
	}
	dataBytes, err := json.Marshal(regPayload)
	if err != nil {
		return fmt.Errorf("marshal registration: %w", err)
	}
	msg := map[string]interface{}{
		"type": "register",
		"from": c.deviceID,
		"to":   "server",
		"data": dataBytes,
	}
	return c.send(msg)
}

func (c *Client) send(payload map[string]interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn == nil {
		return errors.New("connection not established")
	}
	if err := c.conn.WriteJSON(payload); err != nil {
		return fmt.Errorf("send DERP message: %w", err)
	}
	if c.logLevel == LogDebug {
		if data, err := json.Marshal(payload); err == nil {
			c.log(color.HiBlackString(">>> %s", data))
		}
	}
	return nil
}

// SendRouteRequest sends a route_request to create a tunnel route (source=this client, target=targetClient).
// Returns the routeID for use with SendTrafficData.
func (c *Client) SendRouteRequest(organizationID string, targetClient string, externalPort, targetPort int, protocol string) (string, error) {
	if protocol == "" {
		protocol = "TCP"
	}
	routeID := fmt.Sprintf("tunnel_%d", time.Now().UnixNano())
	data, err := json.Marshal(map[string]interface{}{
		"route_id":        routeID,
		"target_client":   targetClient,
		"organization_id": organizationID,
		"external_port":   externalPort,
		"target_port":     targetPort,
		"protocol":        protocol,
	})
	if err != nil {
		return "", err
	}
	if err := c.send(map[string]interface{}{
		"type": "route_request",
		"from": c.deviceID,
		"to":   "server",
		"data": data,
	}); err != nil {
		return "", err
	}
	return routeID, nil
}

// SendTrafficData sends traffic_data for a route (used by tunnel connect to forward bytes).
func (c *Client) SendTrafficData(routeID string, data []byte) error {
	payload, err := json.Marshal(map[string]interface{}{
		"route_id": routeID,
		"data":     data,
	})
	if err != nil {
		return err
	}
	return c.send(map[string]interface{}{
		"type": "traffic_data",
		"from": c.deviceID,
		"to":   "server",
		"data": payload,
	})
}

func (c *Client) handleMessage(msg map[string]interface{}) {
	eventType := EventType(getString(msg["type"]))

	switch eventType {
	case EventPeerList:
		count := len(getSlice(msg["peers"]))
		c.log(color.HiCyanString("Mesh peers online: %d", count))
	case EventPeerJoined:
		peer := msg["peer"]
		c.log(color.HiGreenString("Peer joined: %s", summarizePeer(peer)))
	case EventPeerLeft:
		c.log(color.HiYellowString("Peer left: %s", getString(msg["peer_id"])))
	case EventServiceDiscovery:
		c.log(color.HiBlueString("Service discovery update received"))
	case EventRelayMessage:
		c.log(color.WhiteString("Relay message: %s", summarizeMessage(msg["message"])))
	case EventStatsUpdate:
		c.log(color.HiMagentaString("Mesh stats updated"))
	case EventPong:
		if c.logLevel == LogDebug {
			c.log(color.HiBlackString("< pong >"))
		}
	case EventRouteSetup:
		c.handleRouteSetup(msg)
	case EventRouteResponse:
		c.handleRouteResponse(msg)
	case EventTrafficData:
		c.handleTrafficData(msg)
	case EventError:
		code, detail := parseErrorPayload(msg["data"])
		if detail != "" {
			c.log(color.HiRedString("DERP error: %s — %s", code, detail))
		} else {
			c.log(color.HiRedString("DERP error: %s", code))
		}
	default:
		if c.logLevel == LogDebug {
			c.log(color.HiBlackString("Unhandled message: %+v", msg))
		}
	}
}

func (c *Client) log(message string) {
	if c.logger != nil {
		c.logger.Println(message)
	}
}

func (c *Client) handleRouteSetup(msg map[string]interface{}) {
	data := msg["data"]
	if data == nil {
		return
	}
	var payload struct {
		RouteID        string `json:"route_id"`
		ExternalPort   int    `json:"external_port"`
		TargetPort     int    `json:"target_port"`
		Protocol       string `json:"protocol"`
		OrganizationID string `json:"organization_id"`
	}
	var dataBytes []byte
	switch v := data.(type) {
	case string:
		dataBytes = []byte(v)
	case []byte:
		dataBytes = v
	default:
		dataBytes, _ = json.Marshal(data)
	}
	if err := json.Unmarshal(dataBytes, &payload); err != nil {
		if c.logLevel == LogDebug {
			c.log(color.HiBlackString("route_setup parse error: %v", err))
		}
		return
	}
	if c.TunnelTrafficHandler != nil {
		c.TunnelTrafficHandler(payload.RouteID, payload.TargetPort, payload.ExternalPort, nil)
	} else if c.logLevel == LogDebug {
		c.log(color.HiBlueString("route_setup: %s target_port=%d ext_port=%d", payload.RouteID, payload.TargetPort, payload.ExternalPort))
	}
}

func (c *Client) handleRouteResponse(msg map[string]interface{}) {
	if c.logLevel == LogDebug {
		c.log(color.HiBlueString("route_response received"))
	}
}

func (c *Client) handleTrafficData(msg map[string]interface{}) {
	data := msg["data"]
	if data == nil {
		return
	}
	var payload struct {
		RouteID string `json:"route_id"`
		Data    []byte `json:"data"`
	}
	var dataBytes []byte
	switch v := data.(type) {
	case string:
		dataBytes = []byte(v)
	case []byte:
		dataBytes = v
	default:
		dataBytes, _ = json.Marshal(data)
	}
	if err := json.Unmarshal(dataBytes, &payload); err != nil {
		if c.logLevel == LogDebug {
			c.log(color.HiBlackString("traffic_data parse error: %v", err))
		}
		return
	}
	if c.TunnelTrafficHandler != nil {
		c.TunnelTrafficHandler(payload.RouteID, 0, 0, payload.Data)
	} else if c.logLevel == LogDebug {
		c.log(color.HiBlackString("traffic_data: route=%s len=%d", payload.RouteID, len(payload.Data)))
	}
}

func getString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func parseErrorPayload(data interface{}) (code, detail string) {
	if data == nil {
		return "unknown", ""
	}
	switch v := data.(type) {
	case map[string]interface{}:
		code = getString(v["error"])
		detail = getString(v["detail"])
		if code == "" {
			code = "unknown"
		}
		return code, detail
	case string:
		raw, _ := base64.StdEncoding.DecodeString(v)
		var payload struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return "unknown", string(raw)
		}
		return payload.Error, payload.Detail
	case []byte:
		var payload struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal(v, &payload); err != nil {
			return "unknown", string(v)
		}
		return payload.Error, payload.Detail
	default:
		return "unknown", ""
	}
}

func getSlice(value interface{}) []interface{} {
	switch v := value.(type) {
	case []interface{}:
		return v
	default:
		return nil
	}
}

func summarizePeer(peer interface{}) string {
	data, err := json.Marshal(peer)
	if err != nil {
		return fmt.Sprintf("%v", peer)
	}
	return string(data)
}

func summarizeMessage(msg interface{}) string {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Sprintf("%v", msg)
	}
	return string(data)
}
