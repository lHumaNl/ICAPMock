// Copyright 2026 ICAP Mock

// Package requestinfo extracts bounded metadata from ICAP requests.
package requestinfo

import (
	"context"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

type contentTypeLabelKey struct{}

// WithContentTypeLabel stores the canonical content_type label for this request.
func WithContentTypeLabel(ctx context.Context, label string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contentTypeLabelKey{}, label)
}

// ContextContentTypeLabel returns the request-scoped canonical content_type label.
func ContextContentTypeLabel(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	label, ok := ctx.Value(contentTypeLabelKey{}).(string)
	return label, ok && label != ""
}

// ContentTypeLabel returns the request-scoped label or derives one from the request.
func ContentTypeLabel(ctx context.Context, req *icap.Request) string {
	if label, ok := ContextContentTypeLabel(ctx); ok {
		return label
	}
	return metrics.NormalizeContentTypeLabel(ContentType(req))
}

// ContentType returns the encapsulated HTTP Content-Type relevant to an ICAP request.
func ContentType(req *icap.Request) string {
	if req == nil {
		return ""
	}
	switch req.Method {
	case icap.MethodREQMOD:
		return httpContentType(req.HTTPRequest)
	case icap.MethodRESPMOD:
		return httpContentType(req.HTTPResponse)
	default:
		return ""
	}
}

func httpContentType(message *icap.HTTPMessage) string {
	if message == nil {
		return ""
	}
	if contentType, ok := message.Header.Get("Content-Type"); ok {
		return contentType
	}
	return ""
}
