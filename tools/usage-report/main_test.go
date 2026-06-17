package main

import "testing"

func TestInclusiveDays(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		want           int
	}{
		{"single day", "2026-08-01", "2026-08-01", 1},
		{"week", "2026-08-01", "2026-08-07", 7},
		{"cross month and leap day", "2024-02-28", "2024-03-01", 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inclusiveDays(tt.from, tt.to)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("inclusiveDays=%d want %d", got, tt.want)
			}
		})
	}
}

func TestInclusiveDaysRejectsInvalidRange(t *testing.T) {
	for _, tt := range [][2]string{{"bad", "2026-08-01"}, {"2026-08-02", "2026-08-01"}} {
		if _, err := inclusiveDays(tt[0], tt[1]); err == nil {
			t.Fatalf("expected error for %q..%q", tt[0], tt[1])
		}
	}
}
