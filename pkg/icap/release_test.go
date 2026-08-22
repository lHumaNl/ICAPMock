// Copyright 2026 ICAP Mock

package icap_test

import (
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestHTTPMessageReleaseBodiesResetsLazyBodyState(t *testing.T) {
	message := &icap.HTTPMessage{BodyReader: strings.NewReader("first")}
	assertReleasedHTTPBody(t, message, "first")

	message.ReleaseBodies()
	if message.IsBodyLoaded() {
		t.Fatal("IsBodyLoaded() = true after ReleaseBodies()")
	}
	message.BodyReader = strings.NewReader("second")
	assertReleasedHTTPBody(t, message, "second")
}

func TestRequestReleaseBodiesResetsLazyBodyAndPreservesPreview(t *testing.T) {
	request := &icap.Request{
		BodyReader: strings.NewReader("first"),
		Preview:    0,
		PreviewSet: true,
	}
	assertRequestBody(t, request, "first")

	request.ReleaseBodies()
	if request.IsBodyLoaded() {
		t.Fatal("IsBodyLoaded() = true after ReleaseBodies()")
	}
	if !request.HasPreview() {
		t.Fatal("HasPreview() = false after ReleaseBodies()")
	}
	request.BodyReader = strings.NewReader("second")
	assertRequestBody(t, request, "second")
}

func assertReleasedHTTPBody(t *testing.T, message *icap.HTTPMessage, want string) {
	t.Helper()
	body, err := message.GetBody()
	if err != nil {
		t.Fatalf("GetBody() error = %v", err)
	}
	if string(body) != want {
		t.Fatalf("GetBody() = %q, want %q", body, want)
	}
}

func assertRequestBody(t *testing.T, request *icap.Request, want string) {
	t.Helper()
	body, err := request.GetBody()
	if err != nil {
		t.Fatalf("GetBody() error = %v", err)
	}
	if string(body) != want {
		t.Fatalf("GetBody() = %q, want %q", body, want)
	}
}
