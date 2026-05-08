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
