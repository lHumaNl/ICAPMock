// Copyright 2026 ICAP Mock

package icap

import "sync"

// ReleaseBodies drops buffered body references after a request is fully handled.
// It preserves metadata and headers so post-write metrics can still inspect them.
func (r *Request) ReleaseBodies() {
	if r == nil {
		return
	}
	r.releaseBody()
	r.HTTPRequest.ReleaseBodies()
	r.HTTPResponse.ReleaseBodies()
}

// ReleaseBodies drops buffered body references after a response is written.
func (r *Response) ReleaseBodies() {
	if r == nil {
		return
	}
	r.Body = nil
	r.BodyReader = nil
	r.HTTPRequest.ReleaseBodies()
	r.HTTPResponse.ReleaseBodies()
}

// ReleaseBodies drops buffered body references from an embedded HTTP message.
func (m *HTTPMessage) ReleaseBodies() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.BodyStream != nil {
		_ = m.BodyStream.Release()
	}
	m.Body = nil
	m.BodyReader = nil
	m.BodyStream = nil
	m.bodyErr = nil
	m.bodyLoaded = false
	m.bodyOnce = sync.Once{}
}

func (r *Request) releaseBody() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Body = nil
	r.BodyReader = nil
	r.bodyErr = nil
	r.bodyLoaded = false
	r.bodyOnce = sync.Once{}
}
