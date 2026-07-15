// Copyright 2026 ICAP Mock

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestScenarioRegistriesMatchCanceledContext(t *testing.T) {
	tests := []struct {
		name     string
		registry ScenarioRegistry
	}{
		{name: "standard", registry: NewScenarioRegistry()},
		{name: "sharded", registry: NewShardedScenarioRegistry()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := tt.registry.Match(ctx, &icap.Request{Method: icap.MethodREQMOD})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Match() error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestScenarioRegistriesCancelBlockingBodyMatch(t *testing.T) {
	registries := map[string]ScenarioRegistry{
		"standard": NewScenarioRegistry(),
		"sharded":  NewShardedScenarioRegistry(),
	}
	for name, registry := range registries {
		t.Run(name, func(t *testing.T) {
			addBodyPatternScenario(t, registry)
			reader := newBlockingCloseReader()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			_, err := registry.Match(ctx, bodyPatternTestRequest(reader))
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Match() error = %v, want context.DeadlineExceeded", err)
			}
		})
	}
}

func TestShardedRegistrySerializesConcurrentMutations(t *testing.T) {
	registry := NewShardedScenarioRegistry().(*ShardedScenarioRegistry)
	const writers = 20
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := registry.Add(&Scenario{
				Name:     fmt.Sprintf("scenario-%d", index),
				Match:    MatchRule{HTTPURL: fmt.Sprintf("/file-%d", index)},
				Response: ResponseTemplate{ICAPStatus: 204},
			})
			if err != nil {
				t.Errorf("Add() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	if generation := registry.generation.Load(); generation%2 != 0 {
		t.Fatalf("generation = %d, want stable even generation", generation)
	}
	if got := len(registry.List()); got < writers {
		t.Fatalf("List() count = %d, want at least %d", got, writers)
	}
}

type blockingCloseReader struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingCloseReader() *blockingCloseReader {
	return &blockingCloseReader{closed: make(chan struct{})}
}

func (r *blockingCloseReader) Read([]byte) (int, error) {
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *blockingCloseReader) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}
