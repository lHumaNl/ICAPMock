// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigClient_LoadConfigFile(t *testing.T) {
	ctx := context.Background()
	client := NewConfigClient("localhost", 8080)

	t.Run("successfully loads valid YAML file", func(t *testing.T) {
		tempDir := t.TempDir()
		yamlContent := `server:
  host: "0.0.0.0"
  port: 1344
logging:
  level: "info"
`
		yamlFile := filepath.Join(tempDir, "config.yaml")
		err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("failed to create test YAML file: %v", err)
		}

		content, err := client.LoadConfigFile(ctx, yamlFile)
		if err != nil {
			t.Errorf("LoadConfigFile() error = %v, want nil", err)
		}
		if content != yamlContent {
			t.Errorf("LoadConfigFile() = %v, want %v", content, yamlContent)
		}
	})

	t.Run("successfully loads valid JSON file", func(t *testing.T) {
		tempDir := t.TempDir()
		jsonContent := `{
  "server": {
    "host": "0.0.0.0",
    "port": 1344
  },
  "logging": {
    "level": "info"
  }
}`
		jsonFile := filepath.Join(tempDir, "config.json")
		err := os.WriteFile(jsonFile, []byte(jsonContent), 0644)
		if err != nil {
			t.Fatalf("failed to create test JSON file: %v", err)
		}

		content, err := client.LoadConfigFile(ctx, jsonFile)
		if err != nil {
			t.Errorf("LoadConfigFile() error = %v, want nil", err)
		}
		if content != jsonContent {
			t.Errorf("LoadConfigFile() = %v, want %v", content, jsonContent)
		}
	})

	t.Run("loads .yml file", func(t *testing.T) {
		tempDir := t.TempDir()
		yamlContent := `server:
  host: "0.0.0.0"
`
		ymlFile := filepath.Join(tempDir, "config.yml")
		err := os.WriteFile(ymlFile, []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("failed to create test YML file: %v", err)
		}

		content, err := client.LoadConfigFile(ctx, ymlFile)
		if err != nil {
			t.Errorf("LoadConfigFile() error = %v, want nil", err)
		}
		if content != yamlContent {
			t.Errorf("LoadConfigFile() = %v, want %v", content, yamlContent)
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentFile := filepath.Join(tempDir, "nonexistent.yaml")

		_, err := client.LoadConfigFile(ctx, nonExistentFile)
		if err == nil {
			t.Error("LoadConfigFile() expected error for non-existent file, got nil")
		}

		expectedError := "file not found"
		if !contains(err.Error(), expectedError) {
			t.Errorf("LoadConfigFile() error = %v, want error containing %q", err, expectedError)
		}
	})

	t.Run("returns error for empty file path", func(t *testing.T) {
		_, err := client.LoadConfigFile(ctx, "")
		if err == nil {
			t.Error("LoadConfigFile() expected error for empty path, got nil")
		}

		expectedError := "file path cannot be empty"
		if err.Error() != expectedError {
			t.Errorf("LoadConfigFile() error = %v, want %v", err, expectedError)
		}
	})

	t.Run("returns error for invalid file extension", func(t *testing.T) {
		tempDir := t.TempDir()
		txtFile := filepath.Join(tempDir, "config.txt")
		err := os.WriteFile(txtFile, []byte("some content"), 0644)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		_, err = client.LoadConfigFile(ctx, txtFile)
		if err == nil {
			t.Error("LoadConfigFile() expected error for invalid extension, got nil")
		}

		expectedError := "invalid file extension"
		if !contains(err.Error(), expectedError) {
			t.Errorf("LoadConfigFile() error = %v, want error containing %q", err, expectedError)
		}
	})

	t.Run("returns error for empty configuration file", func(t *testing.T) {
		tempDir := t.TempDir()
		emptyFile := filepath.Join(tempDir, "empty.yaml")
		err := os.WriteFile(emptyFile, []byte(""), 0644)
		if err != nil {
			t.Fatalf("failed to create empty test file: %v", err)
		}

		_, err = client.LoadConfigFile(ctx, emptyFile)
		if err == nil {
			t.Error("LoadConfigFile() expected error for empty file, got nil")
		}

		expectedError := "configuration file is empty"
		if !contains(err.Error(), expectedError) {
			t.Errorf("LoadConfigFile() error = %v, want error containing %q", err, expectedError)
		}
	})

	t.Run("returns error for whitespace-only file", func(t *testing.T) {
		tempDir := t.TempDir()
		whitespaceFile := filepath.Join(tempDir, "whitespace.yaml")
		err := os.WriteFile(whitespaceFile, []byte("   \n\t\n   "), 0644)
		if err != nil {
			t.Fatalf("failed to create whitespace test file: %v", err)
		}

		_, err = client.LoadConfigFile(ctx, whitespaceFile)
		if err == nil {
			t.Error("LoadConfigFile() expected error for whitespace-only file, got nil")
		}

		expectedError := "configuration file is empty"
		if !contains(err.Error(), expectedError) {
			t.Errorf("LoadConfigFile() error = %v, want error containing %q", err, expectedError)
		}
	})

	t.Run("returns error for file exceeding maximum size", func(t *testing.T) {
		tempDir := t.TempDir()
		largeFile := filepath.Join(tempDir, "large.yaml")

		largeContent := make([]byte, maxConfigSize+1)
		for i := range largeContent {
			largeContent[i] = 'a'
		}

		err := os.WriteFile(largeFile, largeContent, 0644)
		if err != nil {
			t.Fatalf("failed to create large test file: %v", err)
		}

		_, err = client.LoadConfigFile(ctx, largeFile)
		if err == nil {
			t.Error("LoadConfigFile() expected error for oversized file, got nil")
		}

		expectedError := "exceeds maximum allowed size"
		if !contains(err.Error(), expectedError) {
			t.Errorf("LoadConfigFile() error = %v, want error containing %q", err, expectedError)
		}
	})

	t.Run("returns error for file path exceeding maximum length", func(t *testing.T) {
		longPath := string(make([]byte, maxFilePathLen+1))
		for i := range longPath {
			longPath = longPath[:i] + "a" + longPath[i+1:]
		}

		_, err := client.LoadConfigFile(ctx, longPath+".yaml")
		if err == nil {
			t.Error("LoadConfigFile() expected error for oversized path, got nil")
		}

		expectedError := "exceeds maximum allowed length"
		if !contains(err.Error(), expectedError) {
			t.Errorf("LoadConfigFile() error = %v, want error containing %q", err, expectedError)
		}
	})

	t.Run("reads file with special characters in content", func(t *testing.T) {
		tempDir := t.TempDir()
		specialContent := `server:
  host: "0.0.0.0"
  port: 1344
  description: "Test with special characters: !@#$%^&*()_+-=[]{}|;':\",./<>?"
  unicode: "Test unicode: 你好世界 🚀"
`
		specialFile := filepath.Join(tempDir, "special.yaml")
		err := os.WriteFile(specialFile, []byte(specialContent), 0644)
		if err != nil {
			t.Fatalf("failed to create special test file: %v", err)
		}

		content, err := client.LoadConfigFile(ctx, specialFile)
		if err != nil {
			t.Errorf("LoadConfigFile() error = %v, want nil", err)
		}
		if content != specialContent {
			t.Errorf("LoadConfigFile() content mismatch")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		tempDir := t.TempDir()
		yamlContent := `server:
  host: "0.0.0.0"
`
		yamlFile := filepath.Join(tempDir, "config.yaml")
		err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err = client.LoadConfigFile(cancelledCtx, yamlFile)
		if err != nil {
			t.Errorf("LoadConfigFile() with canceled context should succeed (file read is not context-aware), got error: %v", err)
		}
	})

	t.Run("idempotent - multiple reads return same content", func(t *testing.T) {
		tempDir := t.TempDir()
		yamlContent := `server:
  host: "0.0.0.0"
  port: 1344
`
		yamlFile := filepath.Join(tempDir, "config.yaml")
		err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		content1, err1 := client.LoadConfigFile(ctx, yamlFile)
		if err1 != nil {
			t.Fatalf("LoadConfigFile() first call error = %v", err1)
		}

		content2, err2 := client.LoadConfigFile(ctx, yamlFile)
		if err2 != nil {
			t.Fatalf("LoadConfigFile() second call error = %v", err2)
		}

		if content1 != content2 {
			t.Errorf("LoadConfigFile() not idempotent: first call = %v, second call = %v", content1, content2)
		}
	})
}

func TestConfigClient_LoadConfigFile_PermissionDenied(t *testing.T) {
	if os.Getenv("SKIP_PERM_TESTS") == "1" {
		t.Skip("Skipping permission tests")
	}

	ctx := context.Background()
	client := NewConfigClient("localhost", 8080)
	tempDir := t.TempDir()

	yamlContent := `server:
  host: "0.0.0.0"
`
	yamlFile := filepath.Join(tempDir, "config.yaml")
	err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	err = os.Chmod(yamlFile, 0000)
	if err != nil {
		t.Fatalf("failed to change file permissions: %v", err)
	}

	_, err = client.LoadConfigFile(ctx, yamlFile)

	os.Chmod(yamlFile, 0644)

	if err == nil {
		t.Skip("File permissions test not applicable on this platform (may not restrict file reading)")
	}

	expectedError := "permission denied"
	if !contains(err.Error(), expectedError) {
		t.Errorf("LoadConfigFile() error = %v, want error containing %q", err, expectedError)
	}
}

func TestConfigClient_LoadConfigFile_InvalidYAML(t *testing.T) {
	ctx := context.Background()
	client := NewConfigClient("localhost", 8080)
	tempDir := t.TempDir()

	invalidYAML := `server:
  host: "0.0.0.0"
  port: 1344
invalid_yaml: [
  unclosed array
`
	invalidFile := filepath.Join(tempDir, "invalid.yaml")
	err := os.WriteFile(invalidFile, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create invalid YAML file: %v", err)
	}

	content, err := client.LoadConfigFile(ctx, invalidFile)
	if err != nil {
		t.Errorf("LoadConfigFile() should read file even with invalid YAML, error = %v", err)
	}
	if content != invalidYAML {
		t.Errorf("LoadConfigFile() = %v, want %v", content, invalidYAML)
	}
}

func TestConfigClient_LoadConfigFile_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	client := NewConfigClient("localhost", 8080)
	tempDir := t.TempDir()

	invalidJSON := `{
  "server": {
    "host": "0.0.0.0",
    "port": 1344
  },
  invalid_json: [
    unclosed array
}`
	invalidFile := filepath.Join(tempDir, "invalid.json")
	err := os.WriteFile(invalidFile, []byte(invalidJSON), 0644)
	if err != nil {
		t.Fatalf("failed to create invalid JSON file: %v", err)
	}

	content, err := client.LoadConfigFile(ctx, invalidFile)
	if err != nil {
		t.Errorf("LoadConfigFile() should read file even with invalid JSON, error = %v", err)
	}
	if content != invalidJSON {
		t.Errorf("LoadConfigFile() = %v, want %v", content, invalidJSON)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
