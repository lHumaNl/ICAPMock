// Package icap provides ICAP (Internet Content Adaptation Protocol) data structures
// and utilities per RFC 3507.
//
// ICAP is a lightweight HTTP-based protocol for providing value-added services
// to HTTP clients and servers. It allows an ICAP client to pass HTTP messages
// to an ICAP server for transformation or other processing.
//
// # Overview
//
// This package provides the core data structures for working with ICAP:
//   - Request: Represents an ICAP request (REQMOD, RESPMOD, OPTIONS)
//   - Response: Represents an ICAP response
//   - Header: Case-insensitive header handling
//   - Chunked encoding: Streaming support for ICAP body transfer
//
// # ICAP Methods
//
// The ICAP protocol defines three methods:
//   - REQMOD: Request Modification - ICAP client sends an HTTP request to the ICAP server
//   - RESPMOD: Response Modification - ICAP client sends an HTTP response to the ICAP server
//   - OPTIONS: The ICAP client asks the ICAP server about its configuration
//
// # Request Format
//
// An ICAP request has the following format:
//
//	REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0
//	Host: icap-server.net
//	Encapsulated: req-hdr=0, req-body=412
//
//	POST /resource HTTP/1.1
//	Host: origin-server.net
//	Content-Length: 1000
//
//	[chunked body]
//
// # Response Codes
//
// ICAP uses similar status codes to HTTP with some additions:
//   - 200 OK: Successful modification
//   - 204 No Content Needed: Original message not modified
//   - 400 Bad Request: Malformed request
//   - 404 ICAP Service not found
//   - 405 Method not allowed
//   - 500 Server error
//   - 501 Not implemented
//   - 502 Bad Gateway
//   - 503 Service overloaded
//   - 505 ICAP version not supported
//
// # Streaming
//
// The package supports streaming for O(1) memory usage when processing large bodies.
// Use ChunkedReader and ChunkedWriter for streaming operations:
//
//	reader := icap.NewChunkedReader(bodyStream)
//	// Read from reader without loading entire body into memory
//
// # Example Usage
//
// Parsing an ICAP request:
//
//	reader := bufio.NewReader(conn)
//	req, err := icap.ParseRequest(reader)
//	if err != nil {
//	    // handle error
//	}
//
// Creating a response:
//
//	resp := icap.NewResponse(icap.StatusOK)
//	resp.SetHeader("ISTag", "W3E4R5")
//	resp.WriteTo(conn)
//
// References:
//   - RFC 3507: Internet Content Adaptation Protocol (ICAP)
//     https://tools.ietf.org/html/rfc3507
package icap
