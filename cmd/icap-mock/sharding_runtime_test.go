// Copyright 2026 ICAP Mock

package main

import (
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
)

func TestScenarioRegistryFactoryUsesStandardRegistryWhenShardingDisabled(t *testing.T) {
	factory := newScenarioRegistryFactory(config.MockMatchingConfig{}, 1024, config.ShardingConfig{Enabled: false})

	if _, ok := factory().(*storage.ShardedScenarioRegistry); ok {
		t.Fatal("registry is sharded, want standard registry when sharding is disabled")
	}
}

func TestScenarioRegistryFactoryThreadsShardingConfig(t *testing.T) {
	sharding := config.ShardingConfig{Enabled: true, ShardCount: 4, CacheSize: 2, EnableCache: false}
	factory := newScenarioRegistryFactory(config.MockMatchingConfig{}, 1024, sharding)

	registry, ok := factory().(*storage.ShardedScenarioRegistry)
	if !ok {
		t.Fatal("registry is not sharded")
	}
	assertShardingConfig(t, registry.Config())
}

func assertShardingConfig(t *testing.T, got storage.ShardingConfig) {
	t.Helper()
	if got.ShardCount != 4 || got.CacheSize != 2 || got.EnableCache {
		t.Fatalf("sharding config = %+v, want shard_count=4 cache_size=2 enable_cache=false", got)
	}
}
