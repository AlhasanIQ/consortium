package benchloop

import (
	"path/filepath"
	"testing"
)

func TestConctlBinaryPath(t *testing.T) {
	workdir := t.TempDir()
	got := conctlBinaryPath(workdir)
	want := filepath.Join(workdir, "bin", "conctl")
	if got != want {
		t.Fatalf("conctlBinaryPath() = %q, want %q", got, want)
	}
}
