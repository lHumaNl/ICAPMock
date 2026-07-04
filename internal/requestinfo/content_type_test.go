// Copyright 2026 ICAP Mock

package requestinfo

import (
	"context"
	"testing"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestContentTypeLabelUsesContextValue(t *testing.T) {
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		HTTPRequest: &icap.HTTPMessage{
			Header: icap.Header{"Content-Type": {"application/json"}},
		},
	}
	ctx := WithContentTypeLabel(context.Background(), metrics.ContentTypeOther)

	if got := ContentTypeLabel(ctx, req); got != metrics.ContentTypeOther {
		t.Fatalf("ContentTypeLabel() = %q, want %q", got, metrics.ContentTypeOther)
	}
}

func TestContentTypeLabelFallsBackToRequest(t *testing.T) {
	req := &icap.Request{
		Method: icap.MethodRESPMOD,
		HTTPResponse: &icap.HTTPMessage{
			Header: icap.Header{"Content-Type": {"Text/HTML; charset=utf-8"}},
		},
	}

	if got := ContentTypeLabel(context.Background(), req); got != "text/html" {
		t.Fatalf("ContentTypeLabel() = %q, want %q", got, "text/html")
	}
}
