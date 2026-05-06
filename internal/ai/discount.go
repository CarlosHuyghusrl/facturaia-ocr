package ai

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// discountPatterns busca líneas de descuento en texto raw OCR.
// Cubre formatos comunes en República Dominicana:
//   - "DESCUENTO 10%: -RD$ 400.00"
//   - "DSCTO: 400"
//   - "DTO: RD$ 400,00"
//   - "Descuento -400"
//   - "DESCUENTO (10%): 400.00"
var discountPatterns = []*regexp.Regexp{
	// Patrón con porcentaje explícito: "DESCUENTO 10%: -RD$ 400.00" o "DTO 10%: 400"
	regexp.MustCompile(`(?i)(?:DESCUENTO|DTO|DSCTO)\s+\d+[%\s]*[:\-\s]+[-]?(?:RD\$|RD|\$)?\s*(\d[\d.,]*)`),
	// Patrón con paréntesis de porcentaje: "DESCUENTO (10%): 400.00"
	regexp.MustCompile(`(?i)(?:DESCUENTO|DTO|DSCTO)\s*\(\d+%\)\s*[:\-]?\s*[-]?(?:RD\$|RD|\$)?\s*(\d[\d.,]*)`),
	// Patrón simple: "DESCUENTO: 400" o "DSCTO: -400.00"
	regexp.MustCompile(`(?i)(?:DESCUENTO|DTO|DSCTO)\s*:\s*[-]?(?:RD\$|RD|\$)?\s*(\d[\d.,]*)`),
	// Patrón con signo negativo antes: "DESCUENTO -400.00"
	regexp.MustCompile(`(?i)(?:DESCUENTO|DTO|DSCTO)\s+[-](?:RD\$|RD|\$)?\s*(\d[\d.,]*)`),
}

// DetectDiscount busca patrones de descuento en rawOCRText y retorna
// el monto detectado como decimal. Retorna 0 si no detecta nada.
// Se usa como fallback cuando Gemini Vision devuelve descuento=0 pero
// el texto raw contiene una línea de descuento (caso Carnamés: "DESCUENTO 10%: -RD$ 400.00").
func DetectDiscount(rawOCRText string) decimal.Decimal {
	for _, p := range discountPatterns {
		m := p.FindStringSubmatch(rawOCRText)
		if len(m) > 1 {
			v, err := parseFloatLatam(m[1])
			if err == nil && v > 0 {
				return decimal.NewFromFloat(v)
			}
		}
	}
	return decimal.Zero
}

// parseFloatLatam convierte strings numéricos con formato latinoamericano a float64.
// Soporta:
//   - "400.00"    → 400.00
//   - "400,00"    → 400.00  (coma decimal, sin separador de miles)
//   - "1,234.56"  → 1234.56 (separador de miles con punto decimal)
//   - "1.234,56"  → 1234.56 (separador de miles con coma decimal)
func parseFloatLatam(s string) (float64, error) {
	s = strings.TrimSpace(s)
	// Detectar si la coma es separador decimal (formato "1234,56" o "1.234,56")
	// vs separador de miles (formato "1,234.56")
	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")

	if lastComma > lastDot {
		// La coma es el separador decimal: "1.234,56" → "1234.56"
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	} else {
		// El punto es el separador decimal (o no hay coma): "1,234.56" → "1234.56"
		s = strings.ReplaceAll(s, ",", "")
	}

	return strconv.ParseFloat(s, 64)
}
