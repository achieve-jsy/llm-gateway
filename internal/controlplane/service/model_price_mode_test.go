package service

import "testing"

// TestValidateModelPricing covers the price_mode / price matrix that gates
// model registration:
//   - manual mode requires at least one positive price;
//   - catalog / video modes allow zero prices (video uses separate billing,
//     not token pricing), so video registration without prices must pass.
func TestValidateModelPricing(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		input    float64
		output   float64
		wantErr  bool
		wantText string
	}{
		{"video zero price passes", "video", 0, 0, false, ""},
		{"video with price passes", "video", 1, 2, false, ""},
		{"catalog zero price passes", "catalog", 0, 0, false, ""},
		{"catalog default mode passes", "", 0, 0, false, ""},
		{"manual with input price passes", "manual", 1, 0, false, ""},
		{"manual with output price passes", "manual", 0, 1, false, ""},
		{"manual both zero rejected", "manual", 0, 0, true, "manual price_mode requires input_price or output_price > 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateModelPricing(c.mode, c.input, c.output)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if err.Error() != c.wantText {
					t.Fatalf("error text mismatch: got %q, want %q", err.Error(), c.wantText)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
