// Copyright 2026 ICAP Mock

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
)

// generateTestCertificate creates a test TLS certificate with specified validity period.
func generateTestCertificate(t *testing.T, validFor time.Duration, certFile, keyFile string) {
	t.Helper()

	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	// Create certificate template
	notBefore := time.Now()
	notAfter := notBefore.Add(validFor)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("failed to generate serial number: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Organization"},
			CommonName:   "test.local",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Create certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	// Write certificate to file
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("failed to create certificate file: %v", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		t.Fatalf("failed to write certificate: %v", err)
	}

	// Write private key to file
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	defer keyOut.Close()

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes}); err != nil {
		t.Fatalf("failed to write private key: %v", err)
	}
}

func TestNewTLSCertificateMonitor(t *testing.T) {
	tests := []struct {
		config  *config.TLSConfig
		name    string
		wantNil bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantNil: true,
		},
		{
			name: "TLS disabled",
			config: &config.TLSConfig{
				Enabled:  false,
				CertFile: "test.crt",
			},
			wantNil: true,
		},
		{
			name: "empty cert file",
			config: &config.TLSConfig{
				Enabled:  true,
				CertFile: "",
			},
			wantNil: true,
		},
		{
			name: "valid config",
			config: &config.TLSConfig{
				Enabled:           true,
				CertFile:          "test.crt",
				CertCheckInterval: 24 * time.Hour,
				ExpiryWarningDays: 30,
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			collector, err := metrics.NewCollector(reg)
			if err != nil {
				t.Fatalf("failed to create metrics collector: %v", err)
			}

			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

			monitor := NewTLSCertificateMonitor(tt.config, logger, collector)
			if (monitor == nil) != tt.wantNil {
				t.Errorf("NewTLSCertificateMonitor() = %v, wantNil %v", monitor, tt.wantNil)
			}
		})
	}
}

func TestTLSCertificateMonitor_GetCertificateDaysToExpiry(t *testing.T) {
	tests := []struct {
		name      string
		validFor  time.Duration
		wantError bool
		minDays   float64
		maxDays   float64
	}{
		{
			name:      "90 days valid",
			validFor:  90 * 24 * time.Hour,
			wantError: false,
			minDays:   89,
			maxDays:   91,
		},
		{
			name:      "30 days valid",
			validFor:  30 * 24 * time.Hour,
			wantError: false,
			minDays:   29,
			maxDays:   31,
		},
		{
			name:      "7 days valid",
			validFor:  7 * 24 * time.Hour,
			wantError: false,
			minDays:   6,
			maxDays:   8,
		},
		{
			name:      "expired certificate",
			validFor:  -24 * time.Hour, // Expired yesterday
			wantError: false,
			minDays:   -2,
			maxDays:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			certFile := filepath.Join(tempDir, "test.crt")
			keyFile := filepath.Join(tempDir, "test.key")

			generateTestCertificate(t, tt.validFor, certFile, keyFile)

			cfg := &config.TLSConfig{
				Enabled:           true,
				CertFile:          certFile,
				CertCheckInterval: time.Hour,
				ExpiryWarningDays: 30,
			}

			reg := prometheus.NewRegistry()
			collector, err := metrics.NewCollector(reg)
			if err != nil {
				t.Fatalf("failed to create metrics collector: %v", err)
			}

			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

			monitor := NewTLSCertificateMonitor(cfg, logger, collector)
			if monitor == nil {
				t.Fatal("monitor should not be nil")
			}

			days, err := monitor.getCertificateDaysToExpiry()
			if (err != nil) != tt.wantError {
				t.Errorf("getCertificateDaysToExpiry() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError {
				if days < tt.minDays || days > tt.maxDays {
					t.Errorf("getCertificateDaysToExpiry() = %v days, want between %v and %v", days, tt.minDays, tt.maxDays)
				}
			}
		})
	}
}

func TestTLSCertificateMonitor_GetCertificateDaysToExpiry_Errors(t *testing.T) {
	tempDir := t.TempDir()
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("failed to create metrics collector: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name     string
		certFile string
	}{
		{
			name:     "non-existent certificate file",
			certFile: filepath.Join(tempDir, "nonexistent.crt"),
		},
		{
			name:     "invalid certificate file",
			certFile: filepath.Join(tempDir, "invalid.crt"),
		},
	}

	// Create invalid certificate file
	if err := os.WriteFile(filepath.Join(tempDir, "invalid.crt"), []byte("invalid cert"), 0o644); err != nil {
		t.Fatalf("failed to create invalid cert file: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.TLSConfig{
				Enabled:           true,
				CertFile:          tt.certFile,
				CertCheckInterval: time.Hour,
				ExpiryWarningDays: 30,
			}

			monitor := NewTLSCertificateMonitor(cfg, logger, collector)
			if monitor == nil {
				t.Fatal("monitor should not be nil")
			}

			_, err := monitor.getCertificateDaysToExpiry()
			if err == nil {
				t.Error("getCertificateDaysToExpiry() expected error, got nil")
			}
		})
	}
}

func TestTLSCertificateMonitor_MetricUpdate(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test.crt")
	keyFile := filepath.Join(tempDir, "test.key")

	// Generate certificate valid for 60 days
	generateTestCertificate(t, 60*24*time.Hour, certFile, keyFile)

	cfg := &config.TLSConfig{
		Enabled:           true,
		CertFile:          certFile,
		CertCheckInterval: time.Hour,
		ExpiryWarningDays: 30,
	}

	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("failed to create metrics collector: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	monitor := NewTLSCertificateMonitor(cfg, logger, collector)
	if monitor == nil {
		t.Fatal("monitor should not be nil")
	}

	// Perform certificate check
	monitor.checkCertificate()

	// Note: We cannot easily verify the metric value without more complex setup,
	// but we can verify the check completes without panic
}

func TestTLSCertificateMonitor_StartStop(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test.crt")
	keyFile := filepath.Join(tempDir, "test.key")

	// Generate certificate valid for 60 days
	generateTestCertificate(t, 60*24*time.Hour, certFile, keyFile)

	cfg := &config.TLSConfig{
		Enabled:           true,
		CertFile:          certFile,
		CertCheckInterval: 100 * time.Millisecond, // Short interval for testing
		ExpiryWarningDays: 30,
	}

	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("failed to create metrics collector: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	monitor := NewTLSCertificateMonitor(cfg, logger, collector)
	if monitor == nil {
		t.Fatal("monitor should not be nil")
	}

	// Start monitor in a goroutine
	done := make(chan struct{})
	go func() {
		monitor.Start()
		close(done)
	}()

	// Let it run for a short time
	time.Sleep(200 * time.Millisecond)

	// Stop the monitor
	monitor.Stop()

	// Verify it stopped
	select {
	case <-done:
		// Monitor stopped successfully
	case <-time.After(1 * time.Second):
		t.Error("monitor did not stop within timeout")
	}
}

func TestTLSCertificateMonitor_WarningThresholds(t *testing.T) {
	tests := []struct {
		name        string
		validFor    time.Duration
		warningDays int
		expectWarn  bool
		expectError bool
	}{
		{
			name:        "valid for 60 days, warning at 30",
			validFor:    60 * 24 * time.Hour,
			warningDays: 30,
			expectWarn:  false,
			expectError: false,
		},
		{
			name:        "valid for 20 days, warning at 30",
			validFor:    20 * 24 * time.Hour,
			warningDays: 30,
			expectWarn:  true,
			expectError: false,
		},
		{
			name:        "valid for 5 days, warning at 30",
			validFor:    5 * 24 * time.Hour,
			warningDays: 30,
			expectWarn:  false, // Will log error instead
			expectError: false,
		},
		{
			name:        "expired certificate",
			validFor:    -24 * time.Hour,
			warningDays: 30,
			expectWarn:  false, // Will log error instead
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			certFile := filepath.Join(tempDir, "test.crt")
			keyFile := filepath.Join(tempDir, "test.key")

			generateTestCertificate(t, tt.validFor, certFile, keyFile)

			cfg := &config.TLSConfig{
				Enabled:           true,
				CertFile:          certFile,
				CertCheckInterval: time.Hour,
				ExpiryWarningDays: tt.warningDays,
			}

			reg := prometheus.NewRegistry()
			collector, err := metrics.NewCollector(reg)
			if err != nil {
				t.Fatalf("failed to create metrics collector: %v", err)
			}

			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

			monitor := NewTLSCertificateMonitor(cfg, logger, collector)
			if monitor == nil {
				t.Fatal("monitor should not be nil")
			}

			// Perform certificate check - we can't verify log output easily,
			// but we verify it completes without panic
			days, err := monitor.getCertificateDaysToExpiry()
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Verify days calculation
			if !tt.expectError {
				expectedDays := tt.validFor.Hours() / 24
				// Allow some tolerance due to timing
				tolerance := 1.0
				if days < expectedDays-tolerance || days > expectedDays+tolerance {
					t.Errorf("days = %v, want approximately %v", days, expectedDays)
				}
			}
		})
	}
}

func TestTLSCertificateMonitor_NilMonitor(_ *testing.T) {
	// Test that nil monitor methods don't panic
	var monitor *TLSCertificateMonitor

	// These should not panic
	monitor.Start()
	monitor.Stop()
}
