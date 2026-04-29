// Copyright 2026 ICAP Mock

package management

import (
	"errors"
	"fmt"
	"os"
)

const (
	// MaxConfigFileBytes caps management config loads before reading file contents.
	MaxConfigFileBytes = 1 << 20
)

var (
	// ErrConfigFileNotRegular is returned when a config path is not a regular file.
	ErrConfigFileNotRegular = errors.New("config path must be a regular file")
	// ErrConfigFileTooLarge is returned when a config file exceeds MaxConfigFileBytes.
	ErrConfigFileTooLarge = errors.New("config file exceeds maximum size")
)

func validateConfigFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrConfigFileNotRegular
	}
	if info.Size() > MaxConfigFileBytes {
		return fmt.Errorf("%w", ErrConfigFileTooLarge)
	}
	return nil
}
