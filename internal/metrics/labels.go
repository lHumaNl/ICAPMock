// Copyright 2026 ICAP Mock

package metrics

import (
	"mime"
	"regexp"
	"strings"
	"sync"
)

const (
	// ContentTypeNone labels requests with no encapsulated HTTP Content-Type.
	ContentTypeNone = "none"
	// ContentTypeInvalid labels malformed Content-Type header values.
	ContentTypeInvalid = "invalid"
	// ContentTypeOther labels safe but unadmitted or unsupported media types.
	ContentTypeOther = "other"

	// OutcomeAllowed labels requests that completed without a blocking response.
	OutcomeAllowed = "allowed"
	// OutcomeBlocked labels requests where ICAP succeeded and returned an HTTP block response.
	OutcomeBlocked = "blocked"
	// OutcomeError labels requests that failed at the ICAP layer.
	OutcomeError = "error"

	maxContentTypeLabelLength   = 120
	maxDynamicContentTypeLabels = 64
	contentTypeParts            = 2
)

var (
	knownContentTypeLabels = map[string]struct{}{
		"application/gzip": {}, "application/javascript": {}, "application/json": {},
		"application/msword": {}, "application/octet-stream": {}, "application/pdf": {},
		"application/problem+json": {}, "application/soap+xml": {}, "application/x-7z-compressed": {},
		"application/x-gzip": {}, "application/x-msdownload": {}, "application/x-rar-compressed": {},
		"application/x-tar": {}, "application/x-www-form-urlencoded": {}, "application/xml": {},
		"application/zip": {}, "audio/mpeg": {}, "audio/mp4": {}, "audio/ogg": {},
		"audio/wav": {}, "font/otf": {}, "font/ttf": {}, "font/woff": {},
		"font/woff2": {}, "image/avif": {}, "image/bmp": {}, "image/gif": {},
		"image/jpeg": {}, "image/png": {}, "image/svg+xml": {}, "image/tiff": {},
		"image/webp": {}, "message/rfc822": {}, "model/gltf+json": {},
		"multipart/alternative": {}, "multipart/form-data": {}, "multipart/mixed": {},
		"multipart/related": {}, "text/css": {}, "text/csv": {}, "text/html": {},
		"text/javascript": {}, "text/markdown": {}, "text/plain": {}, "text/xml": {},
		"video/mp4": {}, "video/mpeg": {}, "video/quicktime": {}, "video/webm": {},
		"video/x-msvideo": {},
	}
	knownContentTypeTops = map[string]struct{}{
		"application": {}, "audio": {}, "font": {}, "image": {}, "message": {},
		"model": {}, "multipart": {}, "text": {}, "video": {},
	}
	mediaTypePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
)

type contentTypeLabelLimiter struct {
	labels map[string]struct{}
	mu     sync.Mutex
}

func newContentTypeLabelLimiter() *contentTypeLabelLimiter {
	return &contentTypeLabelLimiter{labels: make(map[string]struct{})}
}

func (l *contentTypeLabelLimiter) admit(label string) string {
	if isReservedContentTypeLabel(label) || isKnownContentType(label) {
		return label
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.labels[label]; ok {
		return label
	}
	if len(l.labels) >= maxDynamicContentTypeLabels {
		return ContentTypeOther
	}
	l.labels[label] = struct{}{}
	return label
}

// NormalizeContentTypeLabel parses and bounds Content-Type values for metrics labels.
func NormalizeContentTypeLabel(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ContentTypeNone
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return ContentTypeInvalid
	}
	normalized := strings.ToLower(strings.TrimSpace(mediaType))
	return normalizeParsedContentTypeLabel(normalized)
}

// ContentTypeLabel returns the admitted canonical label for a raw Content-Type.
func (c *Collector) ContentTypeLabel(raw string) string {
	return c.contentTypeLabels.admit(NormalizeContentTypeLabel(raw))
}

// RecordRequestForServerWithContentTypeLabel records a request using a precomputed content type label.
func (c *Collector) RecordRequestForServerWithContentTypeLabel(
	server, method, contentTypeLabel, outcome, response, scenario string,
) {
	labels := c.admitScenarioLabels(server, method, contentTypeLabel, outcome, scenario, response)
	c.requestsTotal.WithLabelValues(
		labels.contentType,
		labels.method,
		labels.outcome,
		labels.server,
		labels.response,
		labels.scenario,
	).Inc()
}

func (c *Collector) contentTypeLabel(raw string) string {
	return c.ContentTypeLabel(raw)
}

func (c *Collector) contentTypeLabelValue(label string) string {
	return c.contentTypeLabels.admit(normalizeContentTypeLabelValue(label))
}

func normalizeContentTypeLabelValue(label string) string {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if isReservedContentTypeLabel(normalized) {
		return normalized
	}
	return normalizeParsedContentTypeLabel(normalized)
}

func normalizeParsedContentTypeLabel(label string) string {
	if !validContentTypeLabelSyntax(label) {
		return ContentTypeInvalid
	}
	if _, ok := knownContentTypeTops[contentTypeTop(label)]; !ok {
		return ContentTypeOther
	}
	return boundedContentTypeLabel(label)
}

func validContentTypeLabelSyntax(label string) bool {
	return len(strings.Split(label, "/")) == contentTypeParts &&
		mediaTypePattern.MatchString(label)
}

func contentTypeTop(label string) string {
	return strings.SplitN(label, "/", contentTypeParts)[0]
}

func boundedContentTypeLabel(label string) string {
	if len(label) <= maxContentTypeLabelLength {
		return label
	}
	return label[:maxContentTypeLabelLength]
}

func isReservedContentTypeLabel(label string) bool {
	return label == ContentTypeNone || label == ContentTypeInvalid || label == ContentTypeOther
}

func isKnownContentType(label string) bool {
	_, ok := knownContentTypeLabels[label]
	return ok
}
