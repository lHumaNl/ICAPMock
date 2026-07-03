// Copyright 2026 ICAP Mock

package icap

import (
	"errors"
	"fmt"
	"io"
)

const (
	stackReadBufferSize        = 512
	maxInitialBodyReadCapacity = 1 * 1024 * 1024
)

func readAllLimitedWithCapacity(reader io.Reader, maxBytes, capacity int64) ([]byte, error) {
	initialCapacity := initialReadCapacity(maxBytes, capacity)
	if initialCapacity == 0 {
		return readAllLimitedDefault(reader, maxBytes)
	}
	data := make([]byte, 0, initialCapacity)
	var scratch [stackReadBufferSize]byte
	for {
		n, err := readNextBodyBytes(reader, &data, scratch[:])
		if n > 0 && maxBytes >= 0 && int64(len(data)) > maxBytes {
			return nil, fmt.Errorf("%w: max %d bytes", ErrBodyTooLarge, maxBytes)
		}
		if err != nil {
			return data, normalizeReadAllError(err)
		}
	}
}

func readAllLimitedDefault(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 {
		return io.ReadAll(reader)
	}
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	return bodyWithinLimit(data, maxBytes)
}

func bodyWithinLimit(data []byte, maxBytes int64) ([]byte, error) {
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: max %d bytes", ErrBodyTooLarge, maxBytes)
	}
	return data, nil
}

func readNextBodyBytes(reader io.Reader, data *[]byte, scratch []byte) (int, error) {
	if len(*data) == cap(*data) {
		n, err := reader.Read(scratch)
		*data = append(*data, scratch[:n]...)
		return n, err
	}
	n, err := reader.Read((*data)[len(*data):cap(*data)])
	*data = (*data)[:len(*data)+n]
	return n, err
}

func normalizeReadAllError(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func initialReadCapacity(maxBytes, capacity int64) int {
	if capacity <= 0 || !readCapacityWithinLimit(maxBytes, capacity) {
		return 0
	}
	if capacity > maxInitialBodyReadCapacity {
		return 0
	}
	if int64(int(capacity)) != capacity {
		return 0
	}
	return int(capacity)
}

func readCapacityWithinLimit(maxBytes, capacity int64) bool {
	return maxBytes < 0 || capacity <= maxBytes
}
