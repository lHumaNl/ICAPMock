// Copyright 2026 ICAP Mock

package processor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"regexp"
	"strings"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const multipartMediaPrefix = "multipart/"

var (
	errMultipartContentTypeRequired = errors.New("multipart selector requires multipart Content-Type")
	errMultipartBoundaryRequired    = errors.New("multipart selector requires non-empty boundary")
	errNoMultipartPartsMatched      = errors.New("no multipart parts matched stream selector")
)

type streamHTTPSource struct {
	message *icap.HTTPMessage
	body    []byte
}

func multipartStreamPayload(stream *storage.StreamConfig, req *icap.Request, limit int64) (icap.StreamPayload, error) {
	if multipartRequiresBufferedResolution(stream) {
		return bufferedStreamPayload(stream, req, limit)
	}
	msg, err := streamHTTPMessage(stream.Source.From, req)
	if err != nil {
		return nil, err
	}
	boundary, err := multipartBoundary(msg)
	if err != nil {
		return multipartContentTypeFallback(stream, req, limit, err)
	}
	return liveMultipartStreamPayload(stream, msg, boundary, req, limit)
}

func multipartRequiresBufferedResolution(stream *storage.StreamConfig) bool {
	// raw_file and from fallbacks need access to the original raw source body
	// after selector parsing has failed. A live multipart reader is consumed by
	// that point, so keep the established buffered path for those fallbacks.
	return stream.Fallback.RawFile.IsSet || stream.Fallback.From != ""
}

func liveMultipartStreamPayload(
	stream *storage.StreamConfig,
	msg *icap.HTTPMessage,
	boundary string,
	req *icap.Request,
	limit int64,
) (icap.StreamPayload, error) {
	source, err := icap.NewHTTPMessageBodyStreamPayload(msg, limit)
	if err != nil {
		return nil, streamBodyReadError(err, limit)
	}
	var fallback icap.StreamPayload
	if stream.Fallback.IsSet() {
		fallback, err = multipartFallbackPayload(stream.Fallback, req, limit)
		if err != nil {
			return nil, err
		}
	}
	cfg := stream.Multipart
	return multipartSelectorPayload{source: source, fallback: fallback, cfg: &cfg, boundary: boundary, limit: limit}, nil
}

func multipartContentTypeFallback(
	stream *storage.StreamConfig,
	req *icap.Request,
	limit int64,
	contentTypeErr error,
) (icap.StreamPayload, error) {
	if stream.Fallback.IsSet() {
		return multipartFallbackPayload(stream.Fallback, req, limit)
	}
	return nil, contentTypeErr
}

func multipartFallbackPayload(
	fallback storage.StreamFallbackConfig,
	req *icap.Request,
	limit int64,
) (icap.StreamPayload, error) {
	switch {
	case fallback.Body != "":
		return inlineStreamPayload(fallback.Body, limit)
	case fallback.BodyFile != "":
		return fileStreamPayload(fallback.BodyFile, limit)
	case fallback.From != "":
		return rawHTTPBodyStreamPayload(fallback.From, req, limit)
	}
	return nil, fmt.Errorf("fallback is empty")
}

func resolveMultipartStream(stream *storage.StreamConfig, req *icap.Request, limit int64) ([]byte, error) {
	source, err := streamHTTPSourceFor(stream.Source.From, req, limit)
	if err != nil {
		return nil, err
	}
	selected, err := selectMultipartBody(source, stream.Multipart, limit)
	if err == nil {
		return selected, nil
	}
	if errors.Is(err, errNoMultipartPartsMatched) {
		return resolveMultipartSelectorMiss(stream, source, req, limit)
	}
	if stream.Fallback.IsSet() {
		return resolveStreamFallback(stream.Fallback, source, req, limit)
	}
	return nil, err
}

func resolveMultipartSelectorMiss(
	stream *storage.StreamConfig,
	source streamHTTPSource,
	req *icap.Request,
	limit int64,
) ([]byte, error) {
	if stream.Fallback.IsSet() && !stream.Fallback.RawFile.IsSet {
		return resolveStreamFallback(stream.Fallback, source, req, limit)
	}
	if stream.Multipart.AllowEmpty {
		return nil, nil
	}
	return nil, errNoMultipartPartsMatched
}

func streamHTTPSourceFor(from string, req *icap.Request, limit int64) (streamHTTPSource, error) {
	msg, err := streamHTTPMessage(from, req)
	if err != nil {
		return streamHTTPSource{}, err
	}
	body, err := httpMessageBody(msg, limit)
	return streamHTTPSource{message: msg, body: body}, err
}

func streamHTTPMessage(from string, req *icap.Request) (*icap.HTTPMessage, error) {
	switch from {
	case streamSourceRequestHTTPBody:
		if req.HTTPRequest != nil {
			return req.HTTPRequest, nil
		}
		return nil, fmt.Errorf("request_http_body source requires an encapsulated HTTP request")
	case streamSourceResponseHTTPBody:
		if req.HTTPResponse != nil {
			return req.HTTPResponse, nil
		}
		return nil, fmt.Errorf("response_http_body source requires an encapsulated HTTP response")
	}
	return nil, fmt.Errorf("multipart source %q is unsupported", from)
}

func selectMultipartBody(source streamHTTPSource, cfg storage.StreamMultipartConfig, limit int64) ([]byte, error) {
	reader, err := multipartReader(source)
	if err != nil {
		return nil, err
	}
	selected, err := readSelectedParts(reader, cfg, limit)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, errNoMultipartPartsMatched
	}
	return selected, nil
}

func multipartReader(source streamHTTPSource) (*multipart.Reader, error) {
	boundary, err := multipartBoundary(source.message)
	if err != nil {
		return nil, err
	}
	return multipart.NewReader(bytes.NewReader(source.body), boundary), nil
}

func multipartBoundary(message *icap.HTTPMessage) (string, error) {
	contentType, ok := message.Header.Get("Content-Type")
	if !ok {
		return "", errMultipartContentTypeRequired
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, multipartMediaPrefix) {
		return "", errMultipartContentTypeRequired
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return "", errMultipartBoundaryRequired
	}
	return boundary, nil
}

func readSelectedParts(reader *multipart.Reader, cfg storage.StreamMultipartConfig, limit int64) ([]byte, error) {
	var out bytes.Buffer
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if err := appendSelectedPart(&out, part, cfg, limit); err != nil {
			return nil, err
		}
	}
}

func appendSelectedPart(out *bytes.Buffer, part *multipart.Part, cfg storage.StreamMultipartConfig, limit int64) error {
	if !multipartPartMatches(part, cfg) {
		return nil
	}
	if streamBodyLimitUnlimited(limit) {
		_, err := io.Copy(out, part)
		return err
	}
	remaining := limit - int64(out.Len())
	if remaining < 0 {
		return streamBodyLimitError(limit)
	}
	written, err := io.Copy(out, &io.LimitedReader{R: part, N: remaining + 1})
	if written > remaining {
		return streamBodyLimitError(limit)
	}
	return err
}

func multipartPartMatches(part *multipart.Part, cfg storage.StreamMultipartConfig) bool {
	if fieldSelected(part.FormName(), cfg.Fields) {
		return true
	}
	return fileSelected(part.FileName(), cfg.Files)
}

func fieldSelected(name string, fields []string) bool {
	for _, field := range fields {
		if field == name {
			return true
		}
	}
	return false
}

func fileSelected(filename string, cfg storage.StreamMultipartFilesConfig) bool {
	if !cfg.IsSet || !cfg.Enabled || filename == "" {
		return false
	}
	if len(cfg.Filename) == 0 {
		return true
	}
	return matchesAnyPattern(filename, cfg.Filename)
}

func resolveStreamFallback(
	fallback storage.StreamFallbackConfig,
	source streamHTTPSource,
	req *icap.Request,
	limit int64,
) ([]byte, error) {
	switch {
	case fallback.RawFile.IsSet:
		return resolveRawFileFallback(fallback.RawFile, source)
	case fallback.Body != "":
		return streamBytesWithinLimit([]byte(fallback.Body), limit)
	case fallback.BodyFile != "":
		return readStreamFile(fallback.BodyFile, limit)
	case fallback.From != "":
		return resolveStreamSourceConfig(storage.StreamSourceConfig{From: fallback.From}, req, limit)
	}
	return nil, fmt.Errorf("fallback is empty")
}

func resolveRawFileFallback(cfg storage.StreamRawFileFallback, source streamHTTPSource) ([]byte, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("raw_file fallback is disabled")
	}
	if sourceHasMultipartContentType(source) {
		return nil, fmt.Errorf("raw_file fallback is only available for non-multipart sources")
	}
	if len(cfg.Filename) == 0 {
		return source.body, nil
	}
	filename := contentDispositionFilename(source.message)
	if filename != "" && matchesAnyPattern(filename, cfg.Filename) {
		return source.body, nil
	}
	return nil, fmt.Errorf("raw_file fallback filename did not match")
}

func sourceHasMultipartContentType(source streamHTTPSource) bool {
	raw, ok := source.message.Header.Get("Content-Type")
	if !ok {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return rawContentTypeIsMultipart(raw)
	}
	return strings.HasPrefix(strings.ToLower(mediaType), multipartMediaPrefix)
}

func rawContentTypeIsMultipart(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), multipartMediaPrefix)
}

func contentDispositionFilename(message *icap.HTTPMessage) string {
	raw, ok := message.Header.Get("Content-Disposition")
	if !ok {
		return ""
	}
	_, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	return params["filename"]
}

func matchesAnyPattern(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if regexp.MustCompile(pattern).MatchString(value) {
			return true
		}
	}
	return false
}
