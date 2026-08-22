// Copyright 2026 ICAP Mock

package processor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"

	apperrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const (
	defaultStreamBodyLimit       int64 = 10 * 1024 * 1024
	streamSourceRequestBody            = "request_body"
	streamSourceRequestHTTPBody        = "request_http_body"
	streamSourceResponseBody           = "response_body"
	streamSourceResponseHTTPBody       = "response_http_body"
	streamSourceAdaptedHTTPBody        = "adapted_http_body"
)

var (
	errRepeatedLiveStreamPart   = errors.New("stream parts repeat live HTTP body source")
	errStreamSourceBodyTooLarge = errors.New("stream source body exceeds max_body_size")
)

func (p *MockProcessor) attachStream(resp *icap.Response, tmpl *storage.ResponseTemplate, req *icap.Request) error {
	if tmpl.Stream == nil {
		return nil
	}
	payload, err := resolveStreamPayloadWithLimit(tmpl.Stream, req, p.maxStreamBodySize)
	if err != nil {
		return streamICAPError("failed to resolve stream source", err)
	}
	keepPayload := false
	defer func() {
		if !keepPayload {
			_ = cleanupStreamPayload(payload)
		}
	}()
	target := streamTarget(resp)
	if target == nil {
		return streamICAPError("failed to attach stream", fmt.Errorf("no encapsulated HTTP message"))
	}
	target.Body = nil
	bodyStream, err := newBodyStream(tmpl.Stream, payload, p.streamSelector)
	if err != nil {
		return streamICAPError("failed to configure stream", err)
	}
	target.BodyStream = bodyStream
	keepPayload = true
	if target.Header == nil {
		target.Header = make(icap.Header)
	}
	setStreamContentLength(target.Header, payload)
	if target.BodyStream.Plan.FinishMode() == icap.StreamFinishFIN {
		resp.MarkCloseAfterWrite()
	}
	return nil
}

func resolveStreamPayloadWithLimit(
	stream *storage.StreamConfig,
	req *icap.Request,
	limit int64,
) (icap.StreamPayload, error) {
	if stream.Multipart.IsSet {
		return multipartStreamPayload(stream, req, limit)
	}
	if len(stream.Parts) > 0 {
		return streamPartsPayload(stream.Parts, req, limit)
	}
	if rawHTTPBodySource(stream.Source.From) {
		return rawHTTPBodyStreamPayload(stream.Source.From, req, limit)
	}
	return streamSourcePayload(stream.Source, req, limit)
}

func bufferedStreamPayload(stream *storage.StreamConfig, req *icap.Request, limit int64) (icap.StreamPayload, error) {
	body, err := resolveStreamSourceWithLimit(stream, req, limit)
	if err != nil {
		return nil, err
	}
	return icap.NewBorrowedBytesStreamPayload(body), nil
}

func rawHTTPBodyStreamPayload(from string, req *icap.Request, limit int64) (icap.StreamPayload, error) {
	msg, err := rawHTTPBodyMessage(from, req)
	if err != nil {
		return nil, err
	}
	body, err := httpMessageBody(msg, limit)
	if err != nil {
		return nil, err
	}
	return icap.NewBorrowedBytesStreamPayload(body), nil
}

func streamSourcePayload(
	src storage.StreamSourceConfig,
	req *icap.Request,
	limit int64,
) (icap.StreamPayload, error) {
	switch src.From {
	case streamSourceRequestBody, streamSourceRequestHTTPBody,
		streamSourceResponseBody, streamSourceResponseHTTPBody, streamSourceAdaptedHTTPBody:
		return rawHTTPBodyStreamPayload(src.From, req, limit)
	case "body":
		return inlineStreamPayload(src.Body, limit)
	case "body_file":
		return fileStreamPayload(src.BodyFile, limit)
	default:
		return nil, fmt.Errorf("unsupported source %q", src.From)
	}
}

func inlineStreamPayload(body string, limit int64) (icap.StreamPayload, error) {
	payload := icap.NewBytesStreamPayload([]byte(body))
	return limitStreamPayload(payload, limit)
}

func fileStreamPayload(path string, limit int64) (icap.StreamPayload, error) {
	file, err := openStreamFile(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	payload := newPreparedFilePayload(file, info.Size())
	return limitStreamPayload(payload, limit)
}

func streamPartsPayload(
	parts []storage.StreamPartConfig,
	req *icap.Request,
	limit int64,
) (icap.StreamPayload, error) {
	if err := rejectRepeatedLiveHTTPBodyParts(parts, req); err != nil {
		return nil, err
	}
	payloads := make([]icap.StreamPayload, 0, len(parts))
	for i := range parts {
		payload, err := streamPartPayload(parts[i], req, limit)
		if err != nil {
			_ = cleanupStreamPayloads(payloads)
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	return limitStreamPayload(newPreparedSequencePayload(payloads), limit)
}

func rejectRepeatedLiveHTTPBodyParts(parts []storage.StreamPartConfig, req *icap.Request) error {
	seen := make(map[string]int)
	for i := range parts {
		source, ok := canonicalRawHTTPBodySource(parts[i].From, req.Method)
		if !ok {
			continue
		}
		if first, repeated := seen[source]; repeated {
			if err := rejectRepeatedLivePart(source, first, i, req); err != nil {
				return err
			}
			continue
		}
		seen[source] = i
	}
	return nil
}

func rejectRepeatedLivePart(source string, first, current int, req *icap.Request) error {
	live, err := liveHTTPBodySource(source, req)
	if err != nil || !live {
		return err
	}
	return fmt.Errorf("%w %q at parts[%d] and parts[%d]", errRepeatedLiveStreamPart, source, first, current)
}

func liveHTTPBodySource(source string, req *icap.Request) (bool, error) {
	msg, err := rawHTTPBodyMessage(source, req)
	if err != nil {
		return false, err
	}
	return msg.BodyReader != nil && !messageHasCachedStreamBody(msg), nil
}

func messageHasCachedStreamBody(msg *icap.HTTPMessage) bool {
	return msg.IsBodyLoaded() || len(msg.Body) > 0
}

func streamPartPayload(
	part storage.StreamPartConfig,
	req *icap.Request,
	limit int64,
) (icap.StreamPayload, error) {
	src := storage.StreamSourceConfig(part)
	return streamSourcePayload(src, req, limit)
}

func limitStreamPayload(payload icap.StreamPayload, limit int64) (icap.StreamPayload, error) {
	limited, err := icap.NewLimitedStreamPayload(payload, limit)
	if err != nil {
		_ = cleanupStreamPayload(payload)
		return nil, streamBodyReadError(err, limit)
	}
	return preserveStreamPayloadCleanup(limited, payload), nil
}

func rawHTTPBodyMessage(from string, req *icap.Request) (*icap.HTTPMessage, error) {
	switch from {
	case streamSourceRequestBody, streamSourceRequestHTTPBody:
		return requestHTTPMessage(req)
	case streamSourceResponseBody, streamSourceResponseHTTPBody:
		return responseHTTPMessage(req)
	case streamSourceAdaptedHTTPBody:
		return adaptedHTTPBodyMessage(req)
	}
	return nil, fmt.Errorf("unsupported source %q", from)
}

func adaptedHTTPBodyMessage(req *icap.Request) (*icap.HTTPMessage, error) {
	switch req.Method {
	case icap.MethodREQMOD:
		return requestHTTPMessage(req)
	case icap.MethodRESPMOD:
		return responseHTTPMessage(req)
	default:
		return nil, fmt.Errorf("adapted_http_body source does not support ICAP method %q", req.Method)
	}
}

func requestHTTPMessage(req *icap.Request) (*icap.HTTPMessage, error) {
	if req.HTTPRequest == nil {
		return nil, fmt.Errorf("request body source requires an encapsulated HTTP request")
	}
	return req.HTTPRequest, nil
}

func responseHTTPMessage(req *icap.Request) (*icap.HTTPMessage, error) {
	if req.HTTPResponse == nil {
		return nil, fmt.Errorf("response body source requires an encapsulated HTTP response")
	}
	return req.HTTPResponse, nil
}

func rawHTTPBodySource(from string) bool {
	switch from {
	case streamSourceRequestBody, streamSourceRequestHTTPBody, streamSourceResponseBody,
		streamSourceResponseHTTPBody, streamSourceAdaptedHTTPBody:
		return true
	}
	return false
}

func canonicalRawHTTPBodySource(from, method string) (string, bool) {
	switch from {
	case streamSourceRequestBody, streamSourceRequestHTTPBody:
		return streamSourceRequestHTTPBody, true
	case streamSourceResponseBody, streamSourceResponseHTTPBody:
		return streamSourceResponseHTTPBody, true
	case streamSourceAdaptedHTTPBody:
		if method == icap.MethodREQMOD {
			return streamSourceRequestHTTPBody, true
		}
		if method == icap.MethodRESPMOD {
			return streamSourceResponseHTTPBody, true
		}
	}
	return "", false
}

func setStreamContentLength(header icap.Header, payload icap.StreamPayload) {
	if size, known := payload.SizeHint(); known {
		header.Set("Content-Length", strconv.FormatInt(size, 10))
		return
	}
	header.Del("Content-Length")
}

func resolveStreamSource(stream *storage.StreamConfig, req *icap.Request) ([]byte, error) {
	return resolveStreamSourceWithLimit(stream, req, defaultStreamBodyLimit)
}

func resolveStreamSourceWithLimit(stream *storage.StreamConfig, req *icap.Request, limit int64) ([]byte, error) {
	if len(stream.Parts) > 0 {
		return resolveStreamParts(stream.Parts, req, limit)
	}
	if stream.Multipart.IsSet {
		return resolveMultipartStream(stream, req, limit)
	}
	return resolveStreamSourceConfig(stream.Source, req, limit)
}

func resolveStreamParts(parts []storage.StreamPartConfig, req *icap.Request, limit int64) ([]byte, error) {
	var out bytes.Buffer
	for i := range parts {
		body, err := resolveStreamPart(parts[i], req, limit)
		if err != nil {
			return nil, err
		}
		if err := appendStreamBytes(&out, body, limit); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func resolveStreamPart(part storage.StreamPartConfig, req *icap.Request, limit int64) ([]byte, error) {
	src := storage.StreamSourceConfig(part)
	return resolveStreamSourceConfig(src, req, limit)
}

func resolveStreamSourceConfig(src storage.StreamSourceConfig, req *icap.Request, limit int64) ([]byte, error) {
	switch src.From {
	case streamSourceRequestBody, streamSourceRequestHTTPBody:
		return httpRequestBody(req, limit)
	case streamSourceResponseBody, streamSourceResponseHTTPBody:
		return httpResponseBody(req, limit)
	case streamSourceAdaptedHTTPBody:
		msg, err := adaptedHTTPBodyMessage(req)
		if err != nil {
			return nil, err
		}
		return httpMessageBody(msg, limit)
	case "body":
		return streamBytesWithinLimit([]byte(src.Body), limit)
	case "body_file":
		return readStreamFile(src.BodyFile, limit)
	default:
		return nil, fmt.Errorf("unsupported source %q", src.From)
	}
}

func httpRequestBody(req *icap.Request, limit int64) ([]byte, error) {
	msg, err := requestHTTPMessage(req)
	if err != nil {
		return nil, err
	}
	return httpMessageBody(msg, limit)
}

func httpResponseBody(req *icap.Request, limit int64) ([]byte, error) {
	msg, err := responseHTTPMessage(req)
	if err != nil {
		return nil, err
	}
	return httpMessageBody(msg, limit)
}

func httpMessageBody(msg *icap.HTTPMessage, limit int64) ([]byte, error) {
	if httpBodySizeExceedsLimit(msg, limit) {
		return nil, streamBodyLimitError(limit)
	}

	var (
		body []byte
		err  error
	)
	if streamBodyLimitUnlimited(limit) {
		body, err = msg.GetBody()
	} else {
		body, err = msg.GetBodyLimited(limit)
	}
	if err != nil {
		return nil, streamBodyReadError(err, limit)
	}
	msg.SetLoadedBody(body)
	return body, nil
}

func httpBodySizeExceedsLimit(msg *icap.HTTPMessage, limit int64) bool {
	size, ok := httpBodyContentLength(msg)
	return ok && !streamBodyLimitUnlimited(limit) && size > limit
}

func httpBodyContentLength(msg *icap.HTTPMessage) (int64, bool) {
	if msg == nil || msg.Header == nil {
		return 0, false
	}
	value, ok := msg.Header.Get("Content-Length")
	if !ok {
		return 0, false
	}
	size, err := strconv.ParseInt(value, 10, 64)
	return size, err == nil && size >= 0
}

func readStreamFile(path string, limit int64) ([]byte, error) {
	file, err := openStreamFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return readStreamReader(file, limit)
}

func readStreamReader(reader io.Reader, limit int64) ([]byte, error) {
	if streamBodyLimitUnlimited(limit) {
		return io.ReadAll(reader)
	}
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	return streamBytesWithinLimit(body, limit)
}

func streamBytesWithinLimit(body []byte, limit int64) ([]byte, error) {
	if !streamBodyLimitUnlimited(limit) && int64(len(body)) > limit {
		return nil, streamBodyLimitError(limit)
	}
	return body, nil
}

func appendStreamBytes(out *bytes.Buffer, body []byte, limit int64) error {
	if !streamBodyLimitUnlimited(limit) && int64(out.Len())+int64(len(body)) > limit {
		return streamBodyLimitError(limit)
	}
	_, err := out.Write(body)
	return err
}

func streamBodyReadError(err error, limit int64) error {
	if errors.Is(err, icap.ErrBodyTooLarge) {
		return streamBodyLimitError(limit)
	}
	return err
}

func streamBodyLimitError(limit int64) error {
	return fmt.Errorf("%w: limit %d bytes", errStreamSourceBodyTooLarge, limit)
}

func streamBodyLimitUnlimited(limit int64) bool {
	return limit <= 0
}

func streamTarget(resp *icap.Response) *icap.HTTPMessage {
	if resp.HTTPResponse != nil {
		return resp.HTTPResponse
	}
	return resp.HTTPRequest
}

func streamICAPError(message string, err error) error {
	return apperrors.NewICAPError(
		apperrors.ErrInternalServerError.Code,
		message,
		apperrors.ErrInternalServerError.ICAPStatus,
		err,
	)
}
