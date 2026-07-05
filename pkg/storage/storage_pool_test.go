package storage

import "testing"

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
