// Copyright 2026 ICAP Mock

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// flagWasSet returns true if any named flag was explicitly provided.
func (c *ServerCommand) flagWasSet(names ...string) bool {
	found := false
	c.fs.Visit(func(f *flag.Flag) {
		for _, name := range names {
			if f.Name == name {
				found = true
			}
		}
	})
	return found
}

// applyOverrides applies CLI flag values to the configuration.
func (c *ServerCommand) applyOverrides(cfg *config.Config) {
	c.applyServerOverrides(cfg)
	c.applyLoggingOverrides(cfg)
	c.applyFeatureOverrides(cfg)
}

// parseDurationFlag parses a duration string and warns on error.
func parseDurationFlag(name, value string) (time.Duration, bool) {
	d, err := time.ParseDuration(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: invalid --%s value %q: %v\n", name, value, err)
		return 0, false
	}
	return d, true
}

func (c *ServerCommand) applyServerOverrides(cfg *config.Config) {
	c.applyServerAddressOverrides(cfg)
	c.applyServerLimitOverrides(cfg)
	c.applyServerStreamingOverride(cfg)
	c.applyServerTimeoutOverrides(cfg)
	c.applyServerTLSOverrides(cfg)
}

func (c *ServerCommand) applyServerAddressOverrides(cfg *config.Config) {
	if c.flagWasSet("server.host", "server-host") {
		cfg.Server.Host = c.host
		cfg.Defaults.Host = c.host
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.Host = c.host })
	}
	if c.flagWasSet("server.port", "server-port", "p") {
		cfg.Server.Port = c.port
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.Port = c.port })
	}
}

func (c *ServerCommand) applyServerLimitOverrides(cfg *config.Config) {
	if c.flagWasSet("server.max-connections", "server-max-connections") {
		cfg.Server.MaxConnections = c.maxConns
		cfg.Defaults.MaxConnections = c.maxConns
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.MaxConnections = c.maxConns })
	}
	if c.flagWasSet("server.max-body-size", "server-max-body-size") {
		cfg.Server.MaxBodySize = c.maxBodySize
		cfg.Defaults.SetMaxBodySize(c.maxBodySize)
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.SetMaxBodySize(c.maxBodySize) })
	}
}

func (c *ServerCommand) applyServerStreamingOverride(cfg *config.Config) {
	if c.flagWasSet("server.streaming", "server-streaming") {
		cfg.Server.Streaming = c.streaming
		cfg.Defaults.SetStreaming(c.streaming)
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.SetStreaming(c.streaming) })
	}
}

func (c *ServerCommand) applyServerTimeoutOverrides(cfg *config.Config) {
	c.applyDurationOverride("server.read-timeout", "server-read-timeout", c.readTimeout, func(d time.Duration) {
		cfg.Server.ReadTimeout = d
		cfg.Defaults.ReadTimeout = d
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.ReadTimeout = d })
	})
	c.applyDurationOverride("server.write-timeout", "server-write-timeout", c.writeTimeout, func(d time.Duration) {
		cfg.Server.WriteTimeout = d
		cfg.Defaults.WriteTimeout = d
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.WriteTimeout = d })
	})
	c.applyDurationOverride("server.shutdown-timeout", "server-shutdown-timeout", c.shutdownTimeout, func(d time.Duration) {
		cfg.Server.ShutdownTimeout = d
		cfg.Defaults.ShutdownTimeout = d
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.ShutdownTimeout = d })
	})
}

func (c *ServerCommand) applyDurationOverride(name, alias, value string, apply func(time.Duration)) {
	if !c.flagWasSet(name, alias) {
		return
	}
	if d, ok := parseDurationFlag(name, value); ok {
		apply(d)
	}
}

func (c *ServerCommand) applyServerTLSOverrides(cfg *config.Config) {
	if c.flagWasSet("server.tls.enabled", "server-tls-enabled") {
		cfg.Server.TLS.Enabled = c.tlsEnable
	}
	if c.flagWasSet("server.tls.cert", "server-tls-cert") {
		cfg.Server.TLS.CertFile = c.tlsCert
	}
	if c.flagWasSet("server.tls.key", "server-tls-key") {
		cfg.Server.TLS.KeyFile = c.tlsKey
	}
}

func updateServerEntries(cfg *config.Config, update func(*config.ServerEntryConfig)) {
	for name, entry := range cfg.Servers {
		update(&entry)
		cfg.Servers[name] = entry
	}
}

func (c *ServerCommand) applyLoggingOverrides(cfg *config.Config) {
	if c.debugFlag {
		cfg.Logging.Level = "debug" //nolint:goconst
	} else if c.flagWasSet("logging.level", "logging-level", "l") {
		cfg.Logging.Level = c.logLevel
	}
	if c.flagWasSet("logging.format", "logging-format") {
		cfg.Logging.Format = c.logFormat
	}
	if c.flagWasSet("logging.output", "logging-output") {
		cfg.Logging.Output = c.logOutput
	}
	if c.flagWasSet("logging.max-size", "logging-max-size") {
		cfg.Logging.MaxSize = c.logMaxSize
	}
	if c.flagWasSet("logging.max-backups", "logging-max-backups") {
		cfg.Logging.MaxBackups = c.logMaxBackups
	}
	if c.flagWasSet("logging.max-age", "logging-max-age") {
		cfg.Logging.MaxAge = c.logMaxAge
	}
}

func (c *ServerCommand) applyFeatureOverrides(cfg *config.Config) {
	c.applyMetricsAndMockOverrides(cfg)
	c.applyInfraOverrides(cfg)
}

func (c *ServerCommand) applyMetricsAndMockOverrides(cfg *config.Config) {
	c.applyMetricsOverrides(cfg)
	c.applyMockOverrides(cfg)
}

func (c *ServerCommand) applyMetricsOverrides(cfg *config.Config) {
	if c.flagWasSet("metrics.enabled", "metrics-enabled") {
		cfg.Metrics.Enabled = c.metricsEnabled
	}
	if c.flagWasSet("metrics.host", "metrics-host") {
		cfg.Metrics.Host = c.metricsHost
	}
	if c.flagWasSet("metrics.port", "metrics-port") {
		cfg.Metrics.Port = c.metricsPort
	}
	if c.flagWasSet("metrics.path", "metrics-path") {
		cfg.Metrics.Path = c.metricsPath
	}
	if c.flagWasSet("metrics.endpoint-label-mode", "metrics-endpoint-label-mode") {
		cfg.Metrics.EndpointLabelMode = c.metricsEndpointMode
	}
}

func (c *ServerCommand) applyMockOverrides(cfg *config.Config) {
	if c.flagWasSet("mock.scenarios-dir", "mock-scenarios-dir") {
		cfg.Mock.ScenariosDir = c.scenariosDir
	}
	if c.flagWasSet("mock.timeout", "mock-timeout") {
		if d, ok := parseDurationFlag("mock.timeout", c.mockTimeout); ok {
			cfg.Mock.DefaultTimeout = d
		}
	}
}

func (c *ServerCommand) applyInfraOverrides(cfg *config.Config) {
	c.applyStorageOverrides(cfg)
	c.applyHealthOverrides(cfg)
}

func (c *ServerCommand) applyStorageOverrides(cfg *config.Config) {
	if c.flagWasSet("storage.enabled", "storage-enabled") {
		cfg.Storage.Enabled = c.storageEnabled
	}
	if c.flagWasSet("storage.dir", "storage-dir") {
		cfg.Storage.RequestsDir = c.storageDir
	}
	if c.flagWasSet("storage.max-size", "storage-max-size") {
		cfg.Storage.MaxFileSize = c.storageMaxSize
	}
	if c.flagWasSet("storage.rotate", "storage-rotate") {
		cfg.Storage.RotateAfter = c.storageRotate
	}
}

func (c *ServerCommand) applyHealthOverrides(cfg *config.Config) {
	if c.flagWasSet("health.enabled", "health-enabled") {
		cfg.Health.Enabled = c.healthEnabled
	}
	if c.flagWasSet("health.port", "health-port") {
		cfg.Health.Port = c.healthPort
	}
	c.applyHealthPathOverrides(cfg)
	c.applyPprofOverrides(cfg)
}

func (c *ServerCommand) applyHealthPathOverrides(cfg *config.Config) {
	if c.flagWasSet("health.path", "health-path") {
		cfg.Health.HealthPath = c.healthPath
	}
	if c.flagWasSet("health.ready-path", "health-ready-path") {
		cfg.Health.ReadyPath = c.readyPath
	}
}

func (c *ServerCommand) applyPprofOverrides(cfg *config.Config) {
	if c.flagWasSet("pprof.enabled", "pprof-enabled") {
		cfg.Pprof.Enabled = c.pprofEnabled
	}
}
