package cache

import "testing"

func TestNewAutoUsesMemoryWithoutRedis(t *testing.T) {
	c, err := New(Options{Driver: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Driver(); got != "memory" {
		t.Fatalf("driver = %q", got)
	}
}
