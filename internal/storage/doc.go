// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
//
// This package implements the storage layer for persisting ICAP requests
// and managing mock scenarios. It provides two main components:
//
// # FileStorage
//
// FileStorage implements the Storage interface for persisting requests
// to the filesystem. Each request is stored as a separate JSON file
// with automatic file rotation based on date and request count.
//
// Request files are stored in the format: YYYY-MM-DD_NNN.json
// Example: 2024-01-15_001.json
//
// Example usage:
//
//	cfg := config.StorageConfig{
//	    Enabled:     true,
//	    RequestsDir: "./data/requests",
//	    MaxFileSize: 100 * 1024 * 1024, // 100MB
//	    RotateAfter: 10000,
//	}
//	store, err := storage.NewFileStorage(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer store.Close()
//
//	// Convert and save a request
//	sr := storage.FromICAPRequest(req, 204, processingTime)
//	if err := store.SaveRequest(ctx, sr); err != nil {
//	    log.Printf("Failed to save request: %v", err)
//	}
//
// # ScenarioRegistry
//
// ScenarioRegistry manages mock scenarios loaded from YAML files.
// Scenarios define rules for matching ICAP requests and generating
// mock responses. They are evaluated in priority order (highest first).
//
// Example YAML scenario file:
//
//	scenarios:
//	  - name: "block-malware"
//	    priority: 100
//	    match:
//	      path_pattern: "^/scan.*"
//	      http_method: "POST"
//	      body_pattern: "(?i)(malware|virus)"
//	    response:
//	      icap_status: 200
//	      http_status: 403
//	      headers:
//	        X-Block-Reason: "malware-detected"
//	      body: "Access Denied"
//
// Example usage:
//
//	registry := storage.NewScenarioRegistry()
//	if err := registry.Load("scenarios.yaml"); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Match a request against scenarios
//	scenario, err := registry.Match(req)
//	if err == nil {
//	    // Use scenario.Response to generate response
//	}
//
// # Thread Safety
//
// Both FileStorage and ScenarioRegistry are thread-safe and can be
// used concurrently from multiple goroutines.
//
// # Hot Reloading
//
// ScenarioRegistry supports hot reloading via the Reload() method.
// This allows scenarios to be updated without restarting the server.
package storage
