// Copyright 2026 ICAP Mock

package processor

import (
	"fmt"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func materializeSelectedPreviewBody(
	req *icap.Request,
	response *storage.ResponseTemplate,
	limit int64,
) error {
	if !selectedPreviewNeedsIncomingBody(req, response) {
		return nil
	}
	message, err := adaptedHTTPBodyMessage(req)
	if err != nil {
		return err
	}
	if limit <= 0 {
		_, err = message.GetBody()
	} else {
		_, err = message.GetBodyLimited(limit)
	}
	if err != nil {
		return fmt.Errorf("materializing selected preview body: %w", err)
	}
	return nil
}

func selectedPreviewNeedsIncomingBody(req *icap.Request, response *storage.ResponseTemplate) bool {
	if req == nil || !req.IsPreviewMode() || response == nil || response.Error != "" ||
		response.ICAPStatus == icap.StatusNoContentNeeded {
		return false
	}
	if response.Stream != nil {
		return streamNeedsIncomingBody(response.Stream)
	}
	if response.HTTPBody != "" || response.HTTPBodyFile != "" {
		return false
	}
	if req.IsREQMOD() && response.HTTPStatus > 0 {
		return false
	}
	message, err := adaptedHTTPBodyMessage(req)
	return err == nil && message != nil && message.HasBody()
}

func streamNeedsIncomingBody(stream *storage.StreamConfig) bool {
	if stream == nil {
		return false
	}
	if stream.Multipart.IsSet || incomingStreamSource(stream.Source.From) {
		return true
	}
	for _, part := range stream.Parts {
		if incomingStreamSource(part.From) {
			return true
		}
	}
	return false
}

func incomingStreamSource(source string) bool {
	switch source {
	case streamSourceRequestBody, streamSourceRequestHTTPBody,
		streamSourceResponseBody, streamSourceResponseHTTPBody, streamSourceAdaptedHTTPBody:
		return true
	default:
		return false
	}
}
