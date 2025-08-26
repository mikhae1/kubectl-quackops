package lib

import "testing"

func TestFormatCompactNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{"zero", 0, "0"},
		{"small number", 42, "42"},
		{"hundreds", 950, "950"},
		{"exactly 1k", 1000, "1k"},
		{"small thousands", 1500, "1.5k"},
		{"large thousands", 2900, "2.9k"},
		{"tens of thousands", 15000, "15k"},
		{"hundreds of thousands", 150000, "150k"},
		{"close to million - 950k", 950000, "950k"},
		{"close to million - 990k", 990000, "990k"},
		{"close to million - 999k", 999000, "999k"},
		{"very close to million - 999.9k rounds to 1M", 999900, "1M"},
		{"exactly 1M", 1000000, "1M"},
		{"1.2M", 1200000, "1.2M"},
		{"10M", 10000000, "10M"},
		{"100M", 100000000, "100M"},
		{"exactly 1B", 1000000000, "1B"},
		{"1.5B", 1500000000, "1.5B"},
		{"exactly 1T", 1000000000000, "1T"},
		{"negative small", -42, "-42"},
		{"negative k", -1500, "-1.5k"},
		{"negative M", -2500000, "-2.5M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCompactNumber(tt.input)
			if result != tt.expected {
				t.Errorf("FormatCompactNumber(%d) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatCompactNumberRounding(t *testing.T) {
	// Test specific rounding behavior around thresholds
	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{"999k should stay 999k", 999000, "999k"},
		{"999.5k should round to 1M", 999500, "1M"}, // This is what we want
		{"999.9k should round to 1M", 999900, "1M"}, // This is what we want
		{"1.0M exact", 1000000, "1M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCompactNumber(tt.input)
			if result != tt.expected {
				t.Errorf("FormatCompactNumber(%d) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}