// Copyright 2026 ICAP Mock

package server

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
)

// TLSCertificateMonitor monitors TLS certificate expiry and updates metrics.
// It periodically checks the certificate and logs warnings when it approaches expiry.
type TLSCertificateMonitor struct {
	config        *config.TLSConfig
	logger        *slog.Logger
	metrics       *metrics.Collector
	stopChan      chan struct{}
	certFile      string
	checkInterval time.Duration
	warningDays   int
	stopOnce      sync.Once
}

// NewTLSCertificateMonitor creates a new TLS certificate monitor.
//
// Parameters:
//   - cfg: TLS configuration containing certificate file path and monitoring settings.
//   - logger: Structured logger for monitoring output.
//   - metrics: Metrics collector for updating certificate expiry metrics.
//
// Returns the configured monitor or nil if TLS is not enabled.
func NewTLSCertificateMonitor(cfg *config.TLSConfig, logger *slog.Logger, metrics *metrics.Collector) *TLSCertificateMonitor {
	if cfg == nil || !cfg.Enabled || cfg.CertFile == "" {
		return nil
	}

	return &TLSCertificateMonitor{
		config:        cfg,
		logger:        logger,
		metrics:       metrics,
		stopChan:      make(chan struct{}),
		checkInterval: cfg.CertCheckInterval,
		warningDays:   cfg.ExpiryWarningDays,
		certFile:      cfg.CertFile,
	}
}

// Start begins monitoring the TLS certificate expiry.
// It performs an initial check and then runs periodic checks.
// This method blocks until Stop is called.
func (m *TLSCertificateMonitor) Start() {
	if m == nil {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			if m.logger != nil {
				m.logger.Error("panic in TLS certificate monitor", slog.Any("error", r))
			}
		}
	}()

	// Perform initial check
	m.checkCertificate()

	// Start periodic checks
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkCertificate()
		}
	}
}

// Stop signals the monitor to stop. Safe to call multiple times.
func (m *TLSCertificateMonitor) Stop() {
	if m == nil {
		return
	}

	m.stopOnce.Do(func() {
		close(m.stopChan)
	})
}

// checkCertificate loads and checks the TLS certificate expiry.
// It updates metrics and logs warnings based on the remaining time.
func (m *TLSCertificateMonitor) checkCertificate() {
	days, err := m.getCertificateDaysToExpiry()
	if err != nil {
		// Set metric to -1 to indicate certificate error
		if m.metrics != nil {
			m.metrics.SetTLSCertificateExpiryDays(m.certFile, -1)
		}
		m.logger.Warn("failed to load TLS certificate",
			"cert_file", m.certFile,
			"error", err,
		)
		return
	}

	// Update metric with days to expiry
	if m.metrics != nil {
		m.metrics.SetTLSCertificateExpiryDays(m.certFile, days)
	}

	// Log warnings based on expiry thresholds
	criticalDays := 7
	if days < float64(criticalDays) {
		m.logger.Error("TLS certificate expiring soon",
			"cert_file", m.certFile,
			"days_to_expiry", days,
			"critical_threshold", criticalDays,
		)
	} else if days < float64(m.warningDays) {
		m.logger.Warn("TLS certificate approaching expiry",
			"cert_file", m.certFile,
			"days_to_expiry", days,
			"warning_threshold", m.warningDays,
		)
	} else {
		m.logger.Debug("TLS certificate check",
			"cert_file", m.certFile,
			"days_to_expiry", days,
		)
	}
}

// getCertificateDaysToExpiry loads the TLS certificate and calculates days until expiry.
// Returns the number of days until expiry or an error if the certificate cannot be loaded.
func (m *TLSCertificateMonitor) getCertificateDaysToExpiry() (float64, error) {
	// Check if certificate file exists
	if _, err := os.Stat(m.certFile); os.IsNotExist(err) {
		return 0, fmt.Errorf("certificate file not found: %s", m.certFile)
	}

	// Read certificate file
	certData, err := os.ReadFile(m.certFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read certificate file: %w", err)
	}

	// Decode PEM block
	block, _ := pem.Decode(certData)
	if block == nil {
		return 0, fmt.Errorf("failed to decode PEM certificate")
	}

	// Parse X.509 certificate
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Calculate days until expiry
	duration := time.Until(x509Cert.NotAfter)
	days := duration.Hours() / 24

	// Round to 2 decimal places
	days = math.Round(days*100) / 100

	return days, nil
}
