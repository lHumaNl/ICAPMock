// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/logger"
)

func TestWarnUnauthenticatedManagement(t *testing.T) {
	out, log := warningTestLogger(t)
	warnUnauthenticatedManagement(config.ManagementConfig{Enabled: true}, "", log)
	if !strings.Contains(out.String(), "management API enabled without authentication token") {
		t.Fatalf("warning was not logged: %q", out.String())
	}
}

func TestWarnUnauthenticatedManagementSkipsResolvedToken(t *testing.T) {
	out, log := warningTestLogger(t)
	warnUnauthenticatedManagement(config.ManagementConfig{Enabled: true, Token: "secret"}, "", log)
	if out.Len() != 0 {
		t.Fatalf("warning logged unexpectedly: %q", out.String())
	}
}

func TestWarnUnauthenticatedManagementSkipsTokenEnv(t *testing.T) {
	out, log := warningTestLogger(t)
	t.Setenv("ICAP_MANAGEMENT_TOKEN_TEST", "secret")
	cfg := config.ManagementConfig{Enabled: true, TokenEnv: "ICAP_MANAGEMENT_TOKEN_TEST"}
	warnUnauthenticatedManagement(cfg, "", log)
	if out.Len() != 0 {
		t.Fatalf("warning logged unexpectedly: %q", out.String())
	}
}

func TestWarnUnauthenticatedManagementSkipsFallbackHealthToken(t *testing.T) {
	out, log := warningTestLogger(t)
	warnUnauthenticatedManagement(config.ManagementConfig{Enabled: true}, "health-token", log)
	if out.Len() != 0 {
		t.Fatalf("warning logged unexpectedly: %q", out.String())
	}
}

func TestWarnUnlimitedBodyPattern(t *testing.T) {
	out, log := warningTestLogger(t)
	matching := config.MockMatchingConfig{
		BodyPatternLimit:       config.NewUnlimitedBodySizeLimit(),
		BodyPatternLimitAction: config.BodyPatternLimitActionNoMatch,
	}
	warnUnlimitedBodyPattern(matching, 0, "default", log)
	if !strings.Contains(out.String(), "unlimited body reads") {
		t.Fatalf("warning was not logged: %q", out.String())
	}
}

func TestWarnUnlimitedBodyPatternSkipsServerCap(t *testing.T) {
	out, log := warningTestLogger(t)
	matching := config.MockMatchingConfig{
		BodyPatternLimit:       config.NewUnlimitedBodySizeLimit(),
		BodyPatternLimitAction: config.BodyPatternLimitActionNoMatch,
	}
	warnUnlimitedBodyPattern(matching, 1024, "default", log)
	if out.Len() != 0 {
		t.Fatalf("warning logged unexpectedly: %q", out.String())
	}
}

func warningTestLogger(t *testing.T) (*bytes.Buffer, *logger.Logger) {
	t.Helper()
	out := &bytes.Buffer{}
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "warn", Format: "text"}, out)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	return out, log
}
