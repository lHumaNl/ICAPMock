// Copyright 2026 ICAP Mock

package icap

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"
)

// MaxResponseBodySize is the maximum allowed size for reading response bodies (100 MB).
const MaxResponseBodySize = 100 * 1024 * 1024

// ReadResponse reads and parses an ICAP response from an io.Reader.
func ReadResponse(r io.Reader) (*Response, error) { //nolint:gocyclo // ICAP response parsing is inherently sequential
	data, err := io.ReadAll(io.LimitReader(r, MaxResponseBodySize))
	if err != nil {
		return nil, err
	}
	resp, lines, err := parseResponseStatus(data)
	if err != nil || len(lines) < 2 || len(lines[1]) == 0 {
		return resp, err
	}
	return parseResponseHeadersAndBody(resp, lines[1])
}

func parseResponseStatus(data []byte) (*Response, [][]byte, error) {
	lines := bytes.SplitN(data, []byte("\r\n"), 2)
	if len(lines) < 1 {
		return nil, nil, fmt.Errorf("invalid response: empty")
	}
	parts := strings.SplitN(string(lines[0]), " ", 3)
	if len(parts) < 3 {
		return nil, nil, fmt.Errorf("invalid status line: %s", string(lines[0]))
	}
	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid status code: %s", parts[1])
	}
	resp := NewResponse(statusCode)
	resp.Proto = parts[0]
	return resp, lines, nil
}

func parseResponseHeadersAndBody(resp *Response, data []byte) (*Response, error) {
	headerMap, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(data))).ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading headers: %w", err)
	}
	for k, v := range headerMap {
		resp.Header[CanonicalHeaderKey(k)] = v
	}
	return resp, parseResponseBody(resp, data)
}

func parseResponseBody(resp *Response, data []byte) error {
	encapStr, exists := resp.Header.Get("Encapsulated")
	if !exists {
		return nil
	}
	encap, err := ParseEncapsulatedHeader(encapStr)
	if err != nil {
		return fmt.Errorf("parsing Encapsulated header: %w", err)
	}
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	if headerEnd >= 0 && len(data) > headerEnd+4 && !encap.IsEmpty() && encap.NullBody == encapNotSet {
		resp.Body = data[headerEnd+4:]
	}
	return nil
}
