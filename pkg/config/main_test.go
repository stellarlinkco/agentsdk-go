package config

import (
	"os"
	"testing"
)

// TestMain isolates HOME so user-specific ~/.agents settings do not leak into tests.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "config-test-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
