// Copyright 2026 ICAP Mock

package replay

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Client is an ICAP client for sending requests to ICAP servers.
type Client struct {
	Dialer  *net.Dialer
	Timeout time.Duration
}

// NewClient creates a new ICAP client with the specified timeout.
// If timeout is 0, a default of 30 seconds is used.
func NewClient(timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		Timeout: timeout,
		Dialer: &net.Dialer{
			Timeout: timeout,
		},
	}
}

// Do sends an ICAP request to the specified URL and returns the response.
// The URL should be in the format icap://host:port/service.
//
// Parameters:
//   - ctx: Context for cancellation
//   - targetURL: The ICAP server URL (icap://host:port/service)
//   - req: The ICAP request to send
//
// Returns the response or an error if the request fails.
func (c *Client) Do(ctx context.Context, targetURL string, req *icap.Request) (*icap.Response, error) {
	// Parse the target URL
	host, _, err := parseICAPURL(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}

	// Set the request URI to include the service path
	req.URI = targetURL

	// Establish connection using DialContext for proper context cancellation support.
	// This eliminates the goroutine leak that would occur if we spawned a goroutine
	// for Dial() and then returned early on context cancellation.
	conn, err := c.Dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		// Return context errors directly for proper cancellation propagation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("connecting to %s: %w", host, err)
	}

	defer conn.Close() //nolint:errcheck

	// Set deadlines based on context
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("setting deadline: %w", err)
		}
	} else {
		if err := conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
			return nil, fmt.Errorf("setting deadline: %w", err)
		}
	}

	// Write the request
	if _, err := req.WriteTo(conn); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	// Read the response
	resp, err := icap.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return resp, nil
}

// DoWithBody sends an ICAP request with a fully constructed body.
// This is similar to Do but ensures the body is properly included.
func (c *Client) DoWithBody(ctx context.Context, targetURL string, req *icap.Request) (*icap.Response, error) {
	// Ensure body is loaded if present
	if req.HTTPRequest != nil {
		if _, err := req.HTTPRequest.GetBody(); err != nil {
			return nil, fmt.Errorf("loading HTTP request body: %w", err)
		}
	}
	if req.HTTPResponse != nil {
		if _, err := req.HTTPResponse.GetBody(); err != nil {
			return nil, fmt.Errorf("loading HTTP response body: %w", err)
		}
	}

	return c.Do(ctx, targetURL, req)
}

// Ping tests connectivity to an ICAP server by sending an OPTIONS request.
func (c *Client) Ping(ctx context.Context, targetURL string) error {
	// Validate the URL format
	_, _, err := parseICAPURL(targetURL)
	if err != nil {
		return fmt.Errorf("parsing URL: %w", err)
	}

	// Create OPTIONS request
	req, err := icap.NewRequest(icap.MethodOPTIONS, targetURL)
	if err != nil {
		return fmt.Errorf("creating OPTIONS request: %w", err)
	}

	// Send the request
	resp, err := c.Do(ctx, targetURL, req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// parseICAPURL parses an ICAP URL and returns the host and service path.
// URL format: icap://host:port/service or icap://host/service
func parseICAPURL(url string) (host, service string, err error) {
	// Remove icap:// prefix
	if !strings.HasPrefix(strings.ToLower(url), "icap://") {
		return "", "", fmt.Errorf("invalid ICAP URL scheme, expected icap://")
	}

	rest := url[7:] // Remove "icap://"

	// Find the first slash to separate host from service
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		// No service path
		return rest, "", nil
	}

	host = rest[:slashIdx]
	service = rest[slashIdx:]

	// Add default port if not specified
	if !strings.Contains(host, ":") {
		host = host + ":1344"
	}

	return host, service, nil
}

// Close closes any idle connections in the client.
// This is a no-op for this implementation but provided for interface compatibility.
func (c *Client) Close() error {
	// No connection pooling in this implementation
	return nil
}

// ensure Reader interface.
var _ io.Reader = (*responseReader)(nil)

type responseReader struct {
	reader io.Reader
}

func (r *responseReader) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}
