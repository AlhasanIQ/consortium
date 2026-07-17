package storage

import (
	"path/filepath"
	"testing"
)

func TestSQLiteConnectionPoolLimitsDefaultsFileBackedToFour(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "")
	t.Setenv("DB_MAX_IDLE_CONNS", "")

	maxOpen, maxIdle := sqliteConnectionPoolLimits("./consortium.db")
	if maxOpen != 4 {
		t.Fatalf("maxOpen = %d, want 4", maxOpen)
	}
	if maxIdle != 4 {
		t.Fatalf("maxIdle = %d, want 4", maxIdle)
	}
}

func TestSQLiteConnectionPoolLimitsForceInMemoryToOne(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "8")
	t.Setenv("DB_MAX_IDLE_CONNS", "8")

	maxOpen, maxIdle := sqliteConnectionPoolLimits(":memory:")
	if maxOpen != 1 {
		t.Fatalf("maxOpen = %d, want 1", maxOpen)
	}
	if maxIdle != 1 {
		t.Fatalf("maxIdle = %d, want 1", maxIdle)
	}
}

func TestSQLiteConnectionPoolLimitsHonorsAndClampsEnv(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "6")
	t.Setenv("DB_MAX_IDLE_CONNS", "12")

	maxOpen, maxIdle := sqliteConnectionPoolLimits("./consortium.db")
	if maxOpen != 6 {
		t.Fatalf("maxOpen = %d, want 6", maxOpen)
	}
	if maxIdle != 6 {
		t.Fatalf("maxIdle = %d, want 6", maxIdle)
	}
}

func TestNewStorageAppliesConfiguredOpenConnectionLimit(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "3")
	t.Setenv("DB_MAX_IDLE_CONNS", "3")

	store, err := NewStorage(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	if got := store.DB().Stats().MaxOpenConnections; got != 3 {
		t.Fatalf("MaxOpenConnections = %d, want 3", got)
	}
}

func TestNewStorageKeepsInMemoryDatabaseSingleConnection(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "8")
	t.Setenv("DB_MAX_IDLE_CONNS", "8")

	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	if got := store.DB().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections for :memory: = %d, want 1", got)
	}
}
