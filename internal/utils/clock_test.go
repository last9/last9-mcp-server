package utils

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitClock(t *testing.T) {
	t.Cleanup(func() { t.Setenv(FakeNowEnv, ""); _, _ = InitClock() })

	t.Run("unset uses real time", func(t *testing.T) {
		t.Setenv(FakeNowEnv, "")
		got, err := InitClock()
		if err != nil || got != nil {
			t.Fatalf("got %v, %v; want nil, nil", got, err)
		}
		if d := time.Since(Now()); d < 0 || d > time.Minute {
			t.Fatalf("Now() not real time: off by %v", d)
		}
	})

	t.Run("valid pins Now", func(t *testing.T) {
		t.Setenv(FakeNowEnv, "2026-01-02T03:04:05Z")
		if _, err := InitClock(); err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if !Now().Equal(want) {
			t.Fatalf("Now() = %v, want %v", Now(), want)
		}
	})

	t.Run("invalid fails fast", func(t *testing.T) {
		t.Setenv(FakeNowEnv, "yesterday")
		if _, err := InitClock(); err == nil || !strings.Contains(err.Error(), FakeNowEnv) {
			t.Fatalf("want error naming %s, got %v", FakeNowEnv, err)
		}
	})
}

func TestGetTimeRangeAnchoredAtFakeNow(t *testing.T) {
	t.Cleanup(func() { t.Setenv(FakeNowEnv, ""); _, _ = InitClock() })
	t.Setenv(FakeNowEnv, "2026-01-02T03:04:05Z")
	if _, err := InitClock(); err != nil {
		t.Fatal(err)
	}
	start, end, err := GetTimeRange(map[string]interface{}{"lookback_minutes": 60}, DefaultLookbackMinutes)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !end.Equal(want) || !start.Equal(want.Add(-time.Hour)) {
		t.Fatalf("window = %v..%v, want 1h ending %v", start, end, want)
	}
}

// TestNoRawTimeNowInQueryWindows fails when production code calls time.Now
// outside the allowlist, so new relative windows must use Now().
func TestNoRawTimeNowInQueryWindows(t *testing.T) {
	// Wall-clock uses that are not query windows: timeouts, token expiry.
	allowed := map[string]bool{
		"clock.go":                      true,
		"validate_dashboard_execute.go": true, // elapsed-time measurement
		"test_helpers.go":               true, // fixture token expiry
		"user_token_verifier.go":        true, // token expiry
		"user_token_middleware.go":      true, // token expiry
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "auth" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) && path != root {
			return filepath.SkipDir // auth: token refresh timing
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || allowed[d.Name()] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := parser.ParseFile(token.NewFileSet(), path, src, 0); err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "time.Now") {
				t.Errorf("%s:%d uses time.Now; use utils.Now() for query windows or add the file to the allowlist", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
