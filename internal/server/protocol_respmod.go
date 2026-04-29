// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// maxSegmentedRESPMODRequestBodyBytes bounds the only body section that must be
// buffered to reach a later res-hdr offset. The response body stays streaming.
const maxSegmentedRESPMODRequestBodyBytes = 10 * 1024 * 1024

func needsSegmentedRESPMODParsing(req *icap.Request) bool {
	return req.Method == icap.MethodRESPMOD && req.Encapsulated.HasReqBody() && req.Encapsulated.ResHdr >= 0
}

func parseSegmentedRESPMOD(req *icap.Request) error {
	reader := ensureBufferedBodyReader(req)
	encap := req.Encapsulated
	if err := validateSegmentedRESPMODOffsets(encap); err != nil {
		return err
	}
	if err := discardRESPMODSection(reader, encap.ReqHdr, "request offset"); err != nil {
		return err
	}
	if err := parseSegmentedRESPMODRequest(req, reader); err != nil {
		return err
	}
	return parseSegmentedRESPMODResponse(req, reader)
}

func ensureBufferedBodyReader(req *icap.Request) BufferedReader {
	reader, ok := req.BodyReader.(BufferedReader)
	if ok {
		return reader
	}
	reader = bufio.NewReader(req.BodyReader)
	req.BodyReader = reader
	return reader
}

func parseSegmentedRESPMODRequest(req *icap.Request, reader io.Reader) error {
	encap := req.Encapsulated
	header, err := readRESPMODSection(reader, encap.ReqBody-encap.ReqHdr, maxProtocolHeaderBytes, "HTTP request header")
	if err != nil {
		return err
	}
	req.HTTPRequest, err = parseHTTPRequestSection(header)
	if err != nil {
		return err
	}
	return attachSegmentedRequestBody(req, reader)
}

func attachSegmentedRequestBody(req *icap.Request, reader io.Reader) error {
	bodySize := req.Encapsulated.ResHdr - req.Encapsulated.ReqBody
	body, err := readRESPMODSection(reader, bodySize, maxSegmentedRESPMODRequestBodyBytes, "HTTP request body")
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.HTTPRequest.BodyReader = icap.NewChunkedReader(bytes.NewReader(body))
	}
	return nil
}

func parseSegmentedRESPMODResponse(req *icap.Request, reader BufferedReader) error {
	responseEnd := segmentedRESPMODResponseEnd(req.Encapsulated)
	if responseEnd < 0 {
		return parseEmbeddedHTTPResponseStreaming(req)
	}
	headerSize := responseEnd - req.Encapsulated.ResHdr
	header, err := readRESPMODSection(reader, headerSize, maxProtocolHeaderBytes, "HTTP response header")
	if err != nil {
		return err
	}
	req.HTTPResponse, err = parseHTTPResponseSection(header)
	if err != nil {
		return err
	}
	if req.Encapsulated.HasResBody() {
		req.HTTPResponse.BodyReader = icap.NewChunkedReader(reader)
	}
	return nil
}

func validateSegmentedRESPMODOffsets(encap icap.Encapsulated) error {
	if encap.ReqHdr < 0 || encap.ReqBody < encap.ReqHdr || encap.ResHdr < encap.ReqBody {
		return errors.New("invalid RESPMOD encapsulated offsets")
	}
	if encap.HasResBody() && encap.ResBody < encap.ResHdr {
		return errors.New("invalid RESPMOD response body offset")
	}
	if encap.NullBody >= 0 && encap.NullBody < encap.ResHdr {
		return errors.New("invalid RESPMOD null body offset")
	}
	return nil
}

func segmentedRESPMODResponseEnd(encap icap.Encapsulated) int {
	if encap.HasResBody() {
		return encap.ResBody
	}
	return encap.NullBody
}

func readRESPMODSection(reader io.Reader, size, maxSize int, name string) ([]byte, error) {
	if size < 0 {
		return nil, fmt.Errorf("invalid %s section size: %d", name, size)
	}
	if size > maxSize {
		return nil, fmt.Errorf("%w: %s max %d bytes", ErrBodyTooLarge, name, maxSize)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, fmt.Errorf("reading %s section: %w", name, err)
	}
	return data, nil
}

func discardRESPMODSection(reader io.Reader, size int, name string) error {
	if size < 0 {
		return fmt.Errorf("invalid %s section size: %d", name, size)
	}
	if _, err := io.CopyN(io.Discard, reader, int64(size)); err != nil {
		return fmt.Errorf("reading %s section: %w", name, err)
	}
	return nil
}

func parseHTTPRequestSection(data []byte) (*icap.HTTPMessage, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	line, err := readProtocolLine(reader, maxProtocolRequestLineBytes)
	if err != nil {
		return nil, fmt.Errorf("reading HTTP request line: %w", err)
	}
	message, err := newRequestSectionMessage(line)
	if err != nil {
		return nil, err
	}
	if message.Header, err = parseHeaders(reader); err != nil {
		return nil, fmt.Errorf("parsing HTTP headers: %w", err)
	}
	return message, ensureSectionConsumed(reader, "HTTP request header")
}

func newRequestSectionMessage(line string) (*icap.HTTPMessage, error) {
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return nil, errors.New("invalid HTTP request line")
	}
	return &icap.HTTPMessage{Method: parts[0], URI: parts[1], Proto: parts[2], Header: make(icap.Header)}, nil
}

func parseHTTPResponseSection(data []byte) (*icap.HTTPMessage, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	line, err := readProtocolLine(reader, maxProtocolStatusLineBytes)
	if err != nil {
		return nil, fmt.Errorf("reading HTTP response line: %w", err)
	}
	message, err := newResponseSectionMessage(line)
	if err != nil {
		return nil, err
	}
	if message.Header, err = parseHeaders(reader); err != nil {
		return nil, fmt.Errorf("parsing HTTP headers: %w", err)
	}
	return message, ensureSectionConsumed(reader, "HTTP response header")
}

func newResponseSectionMessage(line string) (*icap.HTTPMessage, error) {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return nil, errors.New("invalid HTTP response line")
	}
	return &icap.HTTPMessage{Proto: parts[0], Status: parts[1], StatusText: parts[2], Header: make(icap.Header)}, nil
}

func ensureSectionConsumed(reader *bufio.Reader, name string) error {
	if _, err := reader.Peek(1); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("checking %s section: %w", name, err)
	}
	return fmt.Errorf("invalid %s offset: section contains extra data", name)
}
