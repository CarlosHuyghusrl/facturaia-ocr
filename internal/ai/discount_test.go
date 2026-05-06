package ai

import (
	"testing"
)

func TestDetectDiscount_Carnames(t *testing.T) {
	// Caso real Carnamés mariscos: subtotal 4000 - descuento 400 = 3600
	raw := "Subtotal: RD$ 4,000.00\nDESCUENTO 10%: -RD$ 400.00\nBase: 3,600.00\nITBIS 18%: 648.00\nTOTAL: 4,248.00"
	got := DetectDiscount(raw)
	if got.InexactFloat64() != 400.00 {
		t.Errorf("TestDetectDiscount_Carnames: expected 400.00, got %v", got)
	}
}

func TestDetectDiscount_NoDiscount(t *testing.T) {
	// Factura normal sin descuento
	raw := "Subtotal: RD$ 1,000.00\nITBIS 18%: 180.00\nTOTAL: 1,180.00"
	got := DetectDiscount(raw)
	if !got.IsZero() {
		t.Errorf("TestDetectDiscount_NoDiscount: expected 0, got %v", got)
	}
}

func TestDetectDiscount_MultiplePatterns(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		expected float64
	}{
		{
			name:     "DSCTO simple",
			raw:      "DSCTO: 250.00\nTOTAL: 750.00",
			expected: 250.00,
		},
		{
			name:     "DTO con signo negativo",
			raw:      "DTO -150\nTotal 850",
			expected: 150.00,
		},
		{
			name:     "DESCUENTO con parentesis porcentaje",
			raw:      "DESCUENTO (10%): 300.00",
			expected: 300.00,
		},
		{
			name:     "DESCUENTO dos puntos simple",
			raw:      "Subtotal 5000\nDESCUENTO: 500\nTotal 4500",
			expected: 500.00,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectDiscount(tc.raw)
			if got.InexactFloat64() != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestDetectDiscount_LatamFormat(t *testing.T) {
	// Formato latinoamericano con coma decimal
	raw := "DESCUENTO: RD$ 400,00"
	got := DetectDiscount(raw)
	if got.InexactFloat64() != 400.00 {
		t.Errorf("TestDetectDiscount_LatamFormat: expected 400.00, got %v", got)
	}
}

func TestParseFloatLatam(t *testing.T) {
	cases := []struct {
		input    string
		expected float64
	}{
		{"400.00", 400.00},
		{"400,00", 400.00},
		{"1,234.56", 1234.56},
		{"1.234,56", 1234.56},
		{"1000", 1000.00},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseFloatLatam(tc.input)
			if err != nil {
				t.Errorf("parseFloatLatam(%q) error: %v", tc.input, err)
			}
			if got != tc.expected {
				t.Errorf("parseFloatLatam(%q): expected %v, got %v", tc.input, tc.expected, got)
			}
		})
	}
}
