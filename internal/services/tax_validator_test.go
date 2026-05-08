package services

import (
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// W17.1 — IA-driven ITBIS exento / sector validation
// ─────────────────────────────────────────────────────────────────────────────

// Test_ITBIS_IA_Sector_Electricidad: Gemini returns sector_proveedor=electricidad,
// categoria_itbis=exento, requiere_correccion=false, ITBIS=0 → no errors, no warnings
// (validator trusts IA — no orange warnings because IA already communicated via warningsIa)
func Test_ITBIS_IA_Sector_Electricidad(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios:     2850.0,
		ITBISFacturado:     0,
		TotalFactura:       2850.0,
		CategoriaITBIS:     "exento",
		SectorProveedor:    "electricidad",
		RequiereCorreccion: false,
		WarningsIA:         []string{"Servicio basico (electricidad/combustible/agua) — ITBIS exento por ley"},
	}

	result := v.Validate(input)

	if !result.Valid {
		t.Errorf("expected valid=true, got errors: %+v", result.Errors)
	}

	// Check: no itbis_mismatch error
	for _, e := range result.Errors {
		if e.Code == "itbis_mismatch" {
			t.Errorf("unexpected itbis_mismatch error for exento provider")
		}
	}

	// IA warningsIa should be propagated as ia_fiscal_warning
	foundIAWarn := false
	for _, w := range result.Warnings {
		if w.Code == "ia_fiscal_warning" {
			foundIAWarn = true
		}
	}
	if !foundIAWarn {
		t.Errorf("expected ia_fiscal_warning from WarningsIA propagation, got warnings: %+v", result.Warnings)
	}
}

// Test_ITBIS_IA_Mixto_Jumbo: Gemini returns categoria_itbis=mixto (supermercado),
// requiere_correccion=false → validator skips strict ITBIS check, no error
func Test_ITBIS_IA_Mixto_Jumbo(t *testing.T) {
	v := NewTaxValidator()
	// Simulate Jumbo invoice where ITBIS doesn't match 18% because of mixed rates
	input := &InvoiceInput{
		MontoServicios:     0,
		MontoBienes:        232563.80,
		ITBISFacturado:     35476.92, // actual Jumbo ITBIS (mixed rate, not exactly 18%)
		TotalFactura:       268040.72,
		CategoriaITBIS:     "mixto",
		SectorProveedor:    "comercio",
		RequiereCorreccion: false,
		WarningsIA:         []string{"Factura mixta — productos a diferentes tasas ITBIS"},
	}

	result := v.Validate(input)

	// Must have no itbis_mismatch error even though 232563.80 * 0.18 = 41861.48 != 35476.92
	for _, e := range result.Errors {
		if e.Code == "itbis_mismatch" || e.Code == "itbis_mismatch_multibucket" {
			t.Errorf("unexpected ITBIS error for mixto invoice: %+v", e)
		}
	}
}

// Test_ITBIS_IA_Sector_Vacio_Cero: ITBIS=0, sector_proveedor="", monto>100
// → orange warning verificar_si_exento (NOT error)
func Test_ITBIS_IA_Sector_Vacio_Cero(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios:     5000.0,
		ITBISFacturado:     0,
		TotalFactura:       5000.0,
		CategoriaITBIS:     "",   // IA didn't identify sector
		SectorProveedor:    "",
		RequiereCorreccion: false,
	}

	result := v.Validate(input)

	// Should have no hard error
	for _, e := range result.Errors {
		if e.Code == "itbis_mismatch" {
			t.Errorf("expected no itbis_mismatch error for unknown sector ITBIS=0, got: %+v", e)
		}
	}

	// Should have orange warning
	foundWarn := false
	for _, w := range result.Warnings {
		if w.Code == "verificar_si_exento" {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Errorf("expected verificar_si_exento warning when sector is unknown and ITBIS=0, got: %+v", result.Warnings)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W17.5 — RNC 8 digits → yellow warning (NOT error)
// ─────────────────────────────────────────────────────────────────────────────

// Test_RNC_8_digitos_Warning: OCR cut one digit from RNC → yellow warning
func Test_RNC_8_digitos_Warning(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		RNCEmisor:      "10144331", // 8 digits — OCR likely cut one
	}

	result := v.Validate(input)

	foundWarn := false
	for _, w := range result.Warnings {
		if w.Code == "rnc_8_digitos_posible_corte" {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Errorf("expected rnc_8_digitos_posible_corte warning for 8-digit RNC, got warnings: %+v", result.Warnings)
	}

	// Must NOT be an error
	for _, e := range result.Errors {
		if e.Code == "rnc_longitud_invalida" {
			t.Errorf("8-digit RNC should be a warning, not an error: %+v", e)
		}
	}
}

// Test_RNC_9_digitos_OK: Valid 9-digit RNC → no RNC error or warning
func Test_RNC_9_digitos_OK(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		RNCEmisor:      "131047939", // 9 digits — valid
	}

	result := v.Validate(input)

	for _, w := range result.Warnings {
		if w.Code == "rnc_8_digitos_posible_corte" || w.Code == "rnc_longitud_invalida" {
			t.Errorf("unexpected RNC warning for valid 9-digit RNC: %+v", w)
		}
	}
	for _, e := range result.Errors {
		if e.Code == "rnc_longitud_invalida" {
			t.Errorf("unexpected RNC error for valid 9-digit RNC: %+v", e)
		}
	}
}

// Test_RNC_11_digitos_OK: Valid 11-digit cedula → no RNC error or warning
func Test_RNC_11_digitos_OK(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		RNCEmisor:      "00112345678", // 11 digits — valid cedula
	}

	result := v.Validate(input)

	for _, w := range result.Warnings {
		if w.Code == "rnc_8_digitos_posible_corte" || w.Code == "rnc_longitud_invalida" {
			t.Errorf("unexpected RNC warning for valid 11-digit RNC: %+v", w)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W17.6 — Date ISO timezone strip → parsed correctly as YYYY-MM-DD
// ─────────────────────────────────────────────────────────────────────────────

// Test_NCFVencimiento_ISO_Sanitize: "2026-01-31T00:00:00Z" must be sanitized to
// "2026-01-31" before parse — if NCF is not expired, no error emitted.
func Test_NCFVencimiento_ISO_Sanitize(t *testing.T) {
	// The sanitizeDate helper must strip the T suffix
	got := sanitizeDate("2026-01-31T00:00:00Z")
	want := "2026-01-31"
	if got != want {
		t.Errorf("sanitizeDate(%q) = %q, want %q", "2026-01-31T00:00:00Z", got, want)
	}
}

// Test_NCFVencimiento_ISO_Timezone_Offset: handles RFC3339 with offset
func Test_NCFVencimiento_ISO_Timezone_Offset(t *testing.T) {
	got := sanitizeDate("2026-01-31T00:00:00-04:00")
	want := "2026-01-31"
	if got != want {
		t.Errorf("sanitizeDate(%q) = %q, want %q", "2026-01-31T00:00:00-04:00", got, want)
	}
}

// Test_NCFVencimiento_PlainDate_Passthrough: plain "YYYY-MM-DD" passes through unchanged
func Test_NCFVencimiento_PlainDate_Passthrough(t *testing.T) {
	got := sanitizeDate("2026-06-30")
	want := "2026-06-30"
	if got != want {
		t.Errorf("sanitizeDate(%q) = %q, want %q", "2026-06-30", got, want)
	}
}

// Test_NCFVencimiento_ISO_In_Validate: full validate with ISO date — no parse error,
// not expired future date should produce no ncf_expired error.
func Test_NCFVencimiento_ISO_In_Validate(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		NCF:            "B0100012345678", // valid format 14 chars — actually B01 + 11 digits = 14
		TipoNCF:        "B01",
		NCFVencimiento: "2099-12-31T00:00:00Z", // future date in ISO format
	}

	result := v.Validate(input)

	for _, e := range result.Errors {
		if e.Code == "ncf_expired" {
			t.Errorf("got ncf_expired error for future date — ISO parse must have failed: %+v", e)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W18 — NCF sanitize (strip whitespace/dashes/uppercase) antes regex check
// ─────────────────────────────────────────────────────────────────────────────

// Test_NCF_Sanitize_With_Dashes: OCR retorna "E32-0036718526" con guiones → no error
func Test_NCF_Sanitize_With_Dashes(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		NCF:            "E32-0036718526", // guiones que OCR puede añadir
	}

	result := v.Validate(input)

	for _, e := range result.Errors {
		if e.Code == "ncf_invalid_format" {
			t.Errorf("NCF con guiones 'E32-0036718526' debe pasar sanitize → no error, got: %+v", e)
		}
	}
}

// Test_NCF_Sanitize_With_Spaces: OCR retorna "E32 0036718526" con espacios → no error
func Test_NCF_Sanitize_With_Spaces(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		NCF:            "E32 0036718526", // espacios que OCR puede añadir
	}

	result := v.Validate(input)

	for _, e := range result.Errors {
		if e.Code == "ncf_invalid_format" {
			t.Errorf("NCF con espacios 'E32 0036718526' debe pasar sanitize → no error, got: %+v", e)
		}
	}
}

// Test_NCF_Sanitize_Lowercase: OCR retorna "e320036718526" en minúscula → no error
func Test_NCF_Sanitize_Lowercase(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		NCF:            "e320036718526", // minúscula — OCR no siempre capitaliza
	}

	result := v.Validate(input)

	for _, e := range result.Errors {
		if e.Code == "ncf_invalid_format" {
			t.Errorf("NCF lowercase 'e320036718526' debe pasar sanitize → no error, got: %+v", e)
		}
	}
}

// Test_NCF_Clean_Still_Valid: NCF limpio "E320036718526" sigue siendo válido (backward compat)
func Test_NCF_Clean_Still_Valid(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		NCF:            "E320036718526", // NCF limpio — debe seguir válido
	}

	result := v.Validate(input)

	for _, e := range result.Errors {
		if e.Code == "ncf_invalid_format" {
			t.Errorf("NCF limpio 'E320036718526' debe ser válido (backward compat), got: %+v", e)
		}
	}
}

// Test_NCF_Invalid_Format_Still_Errors: NCF "ABC123" sigue siendo inválido
func Test_NCF_Invalid_Format_Still_Errors(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 1000.0,
		ITBISFacturado: 180.0,
		TotalFactura:   1180.0,
		NCF:            "ABC123", // formato claramente inválido
	}

	result := v.Validate(input)

	foundError := false
	for _, e := range result.Errors {
		if e.Code == "ncf_invalid_format" {
			foundError = true
		}
	}
	if !foundError {
		t.Errorf("NCF inválido 'ABC123' debe producir error ncf_invalid_format, got errors: %+v", result.Errors)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W18.8 — NCF DGII comprehensive format tests (50+ cases)
// ─────────────────────────────────────────────────────────────────────────────

// TestNCFFormats_DGII_Comprehensive validates the full matrix of NCF formats that
// DGII RD emits in practice: B series (impresos) + E series (electrónicos e-CF),
// with realistic OCR noise (spaces, dashes, dots, tabs, lowercase).
//
// Regex: ^[BE][0-9]{10,12}$ → total length 11–13 chars after sanitize.
// B series: B + type(2) + seq(8) = 11 chars (10 digits)
// E series: E + type(2) + seq(10) = 13 chars (12 digits)
func TestNCFFormats_DGII_Comprehensive(t *testing.T) {
	cases := []struct {
		name        string
		ncf         string
		expectValid bool
		note        string // explanation for boundary cases
	}{
		// ── B series (comprobantes impresos) — válidos ──────────────────────
		{"B01_credito_fiscal", "B0100000001", true, "B01 crédito fiscal — 11 chars"},
		{"B01_with_spaces", "B01 0000 0001", true, "OCR spaces between groups"},
		{"B01_with_dashes", "B01-0000-0001", true, "OCR dashes between groups"},
		{"B01_lowercase", "b0100000001", true, "OCR lowercase b"},
		{"B02_consumidor_final", "B0200000001", true, "B02 consumidor final"},
		{"B03_nota_debito", "B0300000001", true, "B03 nota débito (tipo no en mapa → warning, pero NO error formato)"},
		{"B04_nota_credito", "B0400000001", true, "B04 nota crédito"},
		{"B11_proveedor_informal", "B1100000001", true, "B11 proveedor informal"},
		{"B13_gastos_menores", "B1300000001", true, "B13 gastos menores"},
		{"B14_regimen_especial", "B1400000001", true, "B14 régimen especial"},
		{"B15_gubernamentales", "B1500000001", true, "B15 gubernamentales"},
		{"B16_exportacion", "B1600000001", true, "B16 exportación"},
		{"B01_seq_max_10digits", "B0199999999", true, "B01 secuencia máxima 8 dígitos"},
		{"B01_seq_12digits", "B010000000001", true, "B01 con 12 dígitos total = válido (10,12 rango)"},

		// ── E series (e-CF electrónicos) — válidos ──────────────────────────
		{"E31_factura_electronica", "E310000000001", true, "E31 factura electrónica — 13 chars"},
		{"E32_consumidor_final_e", "E320000000001", true, "E32 consumidor final electrónico"},
		{"E32_jumbo_real", "E320036718526", true, "E32 Jumbo factura real (referencia W18)"},
		{"E32_with_spaces", "E32 0036718526", true, "E32 OCR space after prefix"},
		{"E32_with_dashes", "E32-0036-718526", true, "E32 OCR dashes between groups"},
		{"E32_with_dots", "E32.0036.718526", true, "E32 OCR dots as separators"},
		{"E32_with_tabs", "E32\t0036718526", true, "E32 OCR tab character"},
		{"E33_nota_debito_e", "E330000000001", true, "E33 nota débito electrónico"},
		{"E34_nota_credito_e", "E340000000001", true, "E34 nota crédito electrónico"},
		{"E41_compras", "E410000000001", true, "E41 comprobante compras"},
		{"E43_gastos_menores_e", "E430000000001", true, "E43 gastos menores electrónico"},
		{"E44_regimenes_especiales_e", "E440000000001", true, "E44 regímenes especiales electrónico"},
		{"E45_gubernamentales_e", "E450000000001", true, "E45 gubernamental electrónico"},
		{"E47_formato_valido", "E470000000001", true, "E47 — 12 digits → dentro de rango 10-12"},

		// ── Mixed case + whitespace edge cases ──────────────────────────────
		{"B01_mixed_case", "B0100000001", true, "B01 uppercase normal"},
		{"E32_mixed_case_e", "e320036718526", true, "e32 lowercase e → sanitize uppercase"},
		{"E32_mixed_case_E", "E320036718526", true, "E32 already clean"},
		{"B01_leading_space", " B0100000001", true, "Leading space stripped by TrimSpace in sanitize"},
		{"B01_trailing_space", "B0100000001 ", true, "Trailing space stripped"},
		{"B01_leading_tabs", "\tB0100000001", true, "Leading tab stripped"},
		{"E32_underscore", "E32_0036_718526", true, "Underscore stripped (ncfSanitizeRegex includes _)"},

		// ── E series 10-digit sequences (minimum) ───────────────────────────
		{"E31_10digits_min", "E3100000000", true, "E31 + 10 digits = 11 chars — dentro del mínimo"},
		{"E45_10digits_min", "E4500000000", true, "E45 + 10 digits = 11 chars"},

		// ── Empty / skip cases ───────────────────────────────────────────────
		{"empty_string", "", true, "Empty NCF — validateNCF retorna early (sin error)"},
		{"whitespace_only", "   ", true, "Solo espacios → sanitize da '' → early-return (W18.6: cleanedNCF check antes del regex)"},

		// ── Inválidos — formato claramente incorrecto ───────────────────────
		{"too_short_B01", "B01001", false, "Solo 5 dígitos — demasiado corto"},
		{"missing_letter_start", "0100000001", false, "Sin B/E al inicio"},
		{"wrong_letter_C", "C0100000001", false, "Letra C no válida"},
		{"wrong_letter_X", "X0100000001", false, "Letra X no válida"},
		{"alpha_in_digits", "B010000000A", false, "Letra en posición de dígito"},
		{"only_letters", "BXXXXXXXXXXX", false, "Solo letras en posición dígitos"},
		{"too_long_14digits", "E3100000000000", false, "E31 + 13 digits = 14 chars — excede máximo 13"},
		{"too_long_15chars", "B010000000000001", false, "B01 + 13 digits = 16 chars — demasiado largo"},
		{"junk_string", "ABC123", false, "String basura genérica"},
		{"random_symbols", "@#$%&*()", false, "Símbolos aleatorios"},
		{"numeric_only", "01234567890", false, "Solo dígitos sin letra inicial"},
		{"too_short_9chars", "B01000000", false, "B01 + 6 digits = 9 chars — demasiado corto (mín 11)"},
		{"too_short_10chars", "B010000000", false, "B01 + 7 digits = 10 chars — demasiado corto"},
		{"E_no_digits", "E", false, "Solo E sin dígitos"},
		{"B_no_digits", "B", false, "Solo B sin dígitos"},

		// ── NCF with noise that still resolves to invalid after sanitize ────
		{"dashes_only_invalid", "B01-001", false, "Dashes pero demasiado corto tras sanitize"},
		{"spaces_only_invalid", "B01 001", false, "Spaces pero demasiado corto tras sanitize"},
	}

	v := NewTaxValidator()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := &InvoiceInput{
				NCF:            tc.ncf,
				MontoServicios: 1000,
			}
			result := v.Validate(input)

			hasNCFFormatError := false
			for _, err := range result.Errors {
				if err.Field == "ncf" && err.Code == "ncf_invalid_format" {
					hasNCFFormatError = true
					break
				}
			}

			// Empty/whitespace: after sanitize → "" → early return (valid)
			cleanedNCF := sanitizeNCF(tc.ncf)
			isEffectivelyEmpty := cleanedNCF == ""

			if isEffectivelyEmpty {
				// Empty NCF skips validation — always valid (no format error expected)
				if hasNCFFormatError {
					t.Errorf("[%s] Empty/whitespace NCF should not produce ncf_invalid_format (note: %s)", tc.name, tc.note)
				}
				return
			}

			if tc.expectValid && hasNCFFormatError {
				t.Errorf("[%s] NCF %q (sanitized: %q) expected VALID but got ncf_invalid_format (note: %s)",
					tc.name, tc.ncf, cleanedNCF, tc.note)
			}
			if !tc.expectValid && !hasNCFFormatError {
				t.Errorf("[%s] NCF %q (sanitized: %q) expected INVALID (ncf_invalid_format) but no error produced (note: %s)",
					tc.name, tc.ncf, cleanedNCF, tc.note)
			}
		})
	}
}

// TestSanitizeNCF_Unit validates the sanitizeNCF helper directly (unit tests independent
// of full Validate pipeline).
func TestSanitizeNCF_Unit(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"E32-0036718526", "E320036718526"},
		{"e320036718526", "E320036718526"},
		{"E32 0036 718526", "E320036718526"},
		{"E32.0036.718526", "E320036718526"},
		{"E32_0036_718526", "E320036718526"},
		{"E32\t0036718526", "E320036718526"},
		{"B01-0000-0001", "B0100000001"},  // dashes stripped
		{"b0100000001", "B0100000001"},    // lowercase → upper
		{" B0100000001 ", "B0100000001"}, // trim + clean
		{"E320036718526", "E320036718526"}, // already clean → passthrough
		{"", ""},
		{"   ", ""},
	}

	for _, tc := range cases {
		got := sanitizeNCF(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeNCF(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Backward compat — normal single-rate invoice still works
// ─────────────────────────────────────────────────────────────────────────────

// Test_Normal_Invoice_No_Errors: a plain B01 invoice at 18% with correct amounts
func Test_Normal_Invoice_No_Errors(t *testing.T) {
	v := NewTaxValidator()
	input := &InvoiceInput{
		MontoServicios: 10000.0,
		ITBISFacturado: 1800.0,  // exactly 18%
		TotalFactura:   11800.0,
		RNCEmisor:      "131047939",
		CategoriaITBIS: "general",
		SectorProveedor: "comercio",
	}

	result := v.Validate(input)

	if !result.Valid {
		t.Errorf("expected valid=true for clean invoice, got errors: %+v", result.Errors)
	}
}
