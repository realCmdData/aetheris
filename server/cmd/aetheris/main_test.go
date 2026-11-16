package main

import (
	"slices"
	"testing"
)

// The web address is given once and found again in the state dir; anything but a bare origin is refused.
func TestWebOrigins(t *testing.T) {
	state := t.TempDir()
	if _, err := webOrigins(state, ""); err == nil {
		t.Fatal("no address yet: want an error")
	}
	got, err := webOrigins(state, "https://drive.example.com/, http://localhost:5173")
	if err != nil || !slices.Equal(got, []string{"https://drive.example.com", "http://localhost:5173"}) {
		t.Fatalf("given: %v %v", got, err)
	}
	if again, err := webOrigins(state, ""); err != nil || !slices.Equal(again, got) {
		t.Fatalf("saved: %v %v", again, err)
	}
	for _, bad := range []string{"drive.example.com", "https://drive.example.com/app", "ftp://x"} {
		if _, err := webOrigins(t.TempDir(), bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}
