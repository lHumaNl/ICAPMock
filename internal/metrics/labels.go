// Copyright 2026 ICAP Mock

package metrics

import (
	"net/url"
	pathpkg "path"
	"regexp"
	"strings"
)

const (
	// EndpointLabelModeDefault keeps endpoint labels collapsed to prevent cardinality spikes.
	EndpointLabelModeDefault = "default"
	// EndpointLabelModePath emits the normalized ICAP URI path without query or fragment.
	EndpointLabelModePath = "path"

	// ResponseBodyICAP labels bytes from the ICAP response body.
	ResponseBodyICAP = "icap"
	// ResponseBodyHTTP labels bytes from encapsulated HTTP request or response bodies.
	ResponseBodyHTTP = "http"

	boundedOtherExtensionLabel = "other"
	missingExtensionLabel      = "none"
	maxExtensionLabelLength    = 10
)

var (
	allowedExtensionLabels = map[string]struct{}{
		"7z": {}, "accdb": {}, "apk": {}, "bat": {}, "bin": {}, "bmp": {},
		"csv": {}, "deb": {}, "dll": {}, "doc": {}, "docx": {}, "exe": {},
		"gif": {}, "gz": {}, "gzip": {}, "htm": {}, "html": {}, "jar": {},
		"jpeg": {}, "jpg": {}, "js": {}, "msg": {}, "msi": {}, "odp": {},
		"ods": {}, "odt": {}, "pdf": {}, "png": {}, "ppt": {}, "pptx": {},
		"rar": {}, "rpm": {}, "rtf": {}, "scr": {}, "tar": {}, "txt": {},
		"webp": {}, "xls": {}, "xlsx": {}, "zip": {},
	}
	extensionLabelPattern = regexp.MustCompile(`^[a-z0-9]+$`)
)

// NormalizeEndpointLabel returns a bounded endpoint label for the configured mode.
func NormalizeEndpointLabel(mode, rawURI string) string {
	if mode != EndpointLabelModePath {
		return defaultServerMetricLabel
	}
	return normalizedEndpointPath(rawURI)
}

// ExtractExtension returns a bounded extension label from the normalized path.
func ExtractExtension(rawURI string) string {
	ext := strings.TrimPrefix(pathpkg.Ext(normalizedEndpointPath(rawURI)), ".")
	if ext == "" {
		return missingExtensionLabel
	}
	return normalizeExtensionLabel(ext)
}

// ValidEndpointLabelMode reports whether mode is supported.
func ValidEndpointLabelMode(mode string) bool {
	return mode == EndpointLabelModeDefault || mode == EndpointLabelModePath
}

func normalizedEndpointPath(rawURI string) string {
	pathValue := parsedURIPath(rawURI)
	if pathValue == "" {
		pathValue = "/"
	}
	if !strings.HasPrefix(pathValue, "/") {
		pathValue = "/" + pathValue
	}
	return pathpkg.Clean(pathValue)
}

func parsedURIPath(rawURI string) string {
	uriWithoutFragment := strings.SplitN(rawURI, "#", 2)[0]
	parsed, err := url.Parse(uriWithoutFragment)
	if err == nil && (parsed.Path != "" || parsed.Scheme != "" || parsed.Host != "") {
		return parsed.Path
	}
	return strings.SplitN(uriWithoutFragment, "?", 2)[0]
}

func normalizeBodyLabel(body string) string {
	if body == ResponseBodyHTTP {
		return ResponseBodyHTTP
	}
	return ResponseBodyICAP
}

func normalizeExtensionLabel(ext string) string {
	normalized := strings.ToLower(ext)
	if len(normalized) > maxExtensionLabelLength {
		return boundedOtherExtensionLabel
	}
	if !extensionLabelPattern.MatchString(normalized) {
		return boundedOtherExtensionLabel
	}
	if _, ok := allowedExtensionLabels[normalized]; !ok {
		return boundedOtherExtensionLabel
	}
	return normalized
}
