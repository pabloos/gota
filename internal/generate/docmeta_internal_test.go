package generate

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/pabloos/gota/pkg/model"
)

// TestApplyDocMeta_WarnsUnknownKeys verifies that a gota:doc: top-level key
// gota doesn't apply is reported on stderr rather than dropped silently.
func TestApplyDocMeta_WarnsUnknownKeys(t *testing.T) {
	out := captureStderr(t, func() {
		applyDocMeta(&model.Document{}, &model.DocumentMeta{
			Extra: map[string]any{"mystery": 1, "paths": 2},
		})
	})
	for _, want := range []string{"mystery", "paths"} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr = %q, want it to name the ignored key %q", out, want)
		}
	}
}

func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	f()

	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	return buf.String()
}
