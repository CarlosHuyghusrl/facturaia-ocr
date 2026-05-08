package services

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// ValidationError represents a single validation error
type ValidationError struct {
	Field    string  `json:"field"`
	Code     string  `json:"code"`
	Expected float64 `json:"expected,omitempty"`
	Actual   float64 `json:"actual,omitempty"`
	Message  string  `json:"message,omitempty"`
}

// ValidationWarning represents a non-critical issue
type ValidationWarning struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ComputedValues holds calculated/expected values
type ComputedValues struct {
	BaseGravada    float64 `json:"base_gravada"`
	ITBISEsperado  float64 `json:"itbis_esperado"`
	TotalEsperado  float64 `json:"total_esperado"`
	MontoFacturado float64 `json:"monto_facturado"`
}

// ValidationResult is the response from validation
type ValidationResult struct {
	Valid       bool                `json:"valid"`
	NeedsReview bool                `json:"needs_review"`
	Errors      []ValidationError   `json:"errors"`
	Warnings    []ValidationWarning `json:"warnings"`
	Computed    ComputedValues      `json:"computed"`
}

// InvoiceInput represents the input for validation (from OCR/AI)
type InvoiceInput struct {
	// Base amounts
	MontoServicios float64 `json:"monto_servicios"`
	MontoBienes    float64 `json:"monto_bienes"`
	Descuento      float64 `json:"descuento"`

	// ITBIS
	ITBISFacturado        float64 `json:"itbis_facturado"`
	ITBISTasa             float64 `json:"itbis_tasa"` // 18 (normal) o 16 (zona franca)
	ITBISExento           float64 `json:"itbis_exento"`
	ITBISRetenido         float64 `json:"itbis_retenido"`
	ITBISProporcionalidad float64 `json:"itbis_proporcionalidad"`
	ITBISCosto            float64 `json:"itbis_costo"`

	// ISC
	ISCMonto     float64 `json:"isc_monto"`
	ISCCategoria string  `json:"isc_categoria"`

	// Other taxes
	CDTMonto          float64 `json:"cdt_monto"`
	Cargo911          float64 `json:"cargo_911"`
	PropinaLegal      float64 `json:"propina_legal"`
	OtrosImpuestos    float64 `json:"otros_impuestos"`
	MontoNoFacturable float64 `json:"monto_no_facturable"`

	// ISR retention
	RetencionISRTipo  int     `json:"retencion_isr_tipo"`
	RetencionISRMonto float64 `json:"retencion_isr_monto"`

	// Total
	TotalFactura float64 `json:"total_factura"`

	// Nota credito
	NCFModifica string `json:"ncf_modifica"`
	TipoNCF     string `json:"tipo_ncf"`

	// ITBIS retencion detalle
	ITBISRetenidoPorcentaje int `json:"itbis_retenido_porcentaje"` // 30 o 100

	// NCF
	NCF            string `json:"ncf"`
	NCFVencimiento string `json:"ncf_vencimiento"` // YYYY-MM-DD or ISO RFC3339

	// Payment
	FechaPago string `json:"fecha_pago"` // YYYY-MM-DD

	// Emisor identity — used for W17.5 RNC validation
	NombreEmisor string `json:"nombre_emisor"`
	RNCEmisor    string `json:"rnc_emisor"`

	// W17.1 — IA fiscal skill fields returned by Gemini OCR
	// Gemini identifies ITBIS category + sector to avoid false validation errors.
	// "exento" = proveedor servicio básico (electricidad/combustible/agua) — skip strict ITBIS check
	// "mixto"  = supermercado con productos a tasas mixtas 18%+16%+0% — skip strict ITBIS check
	// "general" or "" = single-rate 18% — apply normal validation
	CategoriaITBIS string `json:"categoria_itbis"` // general|reducido|exento|mixto

	// sector_proveedor identified by IA: electricidad|combustible|agua|salud|educacion|comercio|otros
	// Sectors other than "comercio" are typically ITBIS-exempt by Dominican law.
	SectorProveedor string `json:"sector_proveedor"`

	// requiere_correccion is set by IA to true ONLY when it detects a real error.
	// false = IA considers invoice valid (possibly with warnings). Validator respects this.
	RequiereCorreccion bool `json:"requiere_correccion"`

	// warnings_ia contains informational messages from the IA fiscal analysis.
	// These are surfaced as orange warnings to the user, never blocking errors.
	WarningsIA []string `json:"warnings_ia"`
}

// TaxValidator validates Dominican invoice tax fields
type TaxValidator struct {
	tolerance float64 // percentage tolerance (0.05 = 5%)
}

// NewTaxValidator creates a new validator with default 5% tolerance
func NewTaxValidator() *TaxValidator {
	return &TaxValidator{tolerance: 0.05}
}

// Validate performs all cross-validations on invoice data
func (v *TaxValidator) Validate(input *InvoiceInput) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		NeedsReview: false,
		Errors:      []ValidationError{},
		Warnings:    []ValidationWarning{},
	}

	// Propagate IA warnings as orange (non-blocking) warnings first
	for _, w := range input.WarningsIA {
		if w != "" {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "warnings_ia",
				Code:    "ia_fiscal_warning",
				Message: w,
			})
		}
	}

	// Calculate computed values
	baseGravada := input.MontoServicios + input.MontoBienes - input.Descuento - input.ITBISExento
	if baseGravada < 0 {
		baseGravada = 0
	}
	montoFacturado := input.MontoServicios + input.MontoBienes - input.Descuento

	// ITBIS rate: 18% normal, 16% for zona franca
	itbisTasa := 0.18
	if input.ITBISTasa == 16 {
		itbisTasa = 0.16
	}
	itbisEsperado := baseGravada * itbisTasa

	totalEsperado := montoFacturado + input.ITBISFacturado + input.ISCMonto +
		input.CDTMonto + input.Cargo911 + input.PropinaLegal + input.OtrosImpuestos

	result.Computed = ComputedValues{
		BaseGravada:    round2(baseGravada),
		ITBISEsperado:  round2(itbisEsperado),
		TotalEsperado:  round2(totalEsperado),
		MontoFacturado: round2(montoFacturado),
	}

	// 1. Validate ITBIS vs Base Imponible
	v.validateITBIS(input, result, baseGravada, itbisEsperado)

	// 2. Validate Total Factura
	v.validateTotal(input, result, totalEsperado)

	// 3. Validate Propina Legal (10%)
	v.validatePropina(input, result, montoFacturado)

	// 4. Validate Telecom (ISC + CDT)
	v.validateTelecom(input, result, baseGravada)

	// 5. Validate NCF format and expiration
	v.validateNCF(input, result)

	// 6. Validate Retenciones
	v.validateRetenciones(input, result)

	// 7. Validate field coherence
	v.validateCoherence(input, result)

	// 8. Validate ISC Seguros
	v.validateISCSeguros(input, result, baseGravada)

	// 9. Validate Nota Credito
	v.validateNotaCredito(input, result)

	// 10. Validate Exportaciones
	v.validateExportaciones(input, result)

	// 11. Validate Gubernamentales
	v.validateGubernamentales(input, result)

	// 12. Validate ITBIS Retenido porcentaje
	v.validateITBISRetenidoPorcentaje(input, result)

	// 13. Validate RNC emisor length (W17.5)
	v.validateRNCLength(input, result)

	// Set final status
	result.Valid = len(result.Errors) == 0
	result.NeedsReview = len(result.Warnings) > 0

	return result
}

// validateITBIS checks ITBIS matches expected rate of base gravada.
//
// W17.1 (IA-driven): If Gemini fiscal skill identified the invoice as exento or mixto,
// skip strict validation — IA already made the call. Only emit orange warning when
// ITBIS=0 and sector is unknown (sector_proveedor == "" or "comercio").
func (v *TaxValidator) validateITBIS(input *InvoiceInput, result *ValidationResult, baseGravada, itbisEsperado float64) {
	if baseGravada <= 0 {
		return
	}

	// W17.1 — IA fiscal skill: respect IA categorization
	// "exento" = proveedor servicio básico — skip strict ITBIS, trust IA
	// "mixto"  = supermercado multi-tasa — skip strict ITBIS, trust IA
	categoriaLower := strings.ToLower(input.CategoriaITBIS)
	if categoriaLower == "exento" || categoriaLower == "mixto" {
		return
	}

	// If IA identified a non-comercio sector and did NOT flag requiere_correccion, trust IA
	sectorLower := strings.ToLower(input.SectorProveedor)
	if sectorLower != "" && sectorLower != "comercio" && !input.RequiereCorreccion {
		return
	}

	// W17.1 — If ITBIS=0 but IA did not identify sector → orange warning (NOT error)
	// This covers unknown providers that may be exempt (user should verify manually)
	if input.ITBISFacturado == 0 {
		montoFacturado := input.MontoServicios + input.MontoBienes - input.Descuento
		if montoFacturado > 100 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "itbis_facturado",
				Code:    "verificar_si_exento",
				Message: "ITBIS=0 detectado — verificar si proveedor es servicio básico exento",
			})
		}
		return
	}

	// Skip si la factura tiene productos exentos, ISC, o monto no facturable
	// (mezcla de exento+gravado hace que ITBIS no sea exactamente 18% del total)
	if input.ITBISExento > 0 || input.MontoNoFacturable > 0 || input.ISCMonto > 0 {
		// Solo emitir warning, no error
		diff := math.Abs(input.ITBISFacturado - itbisEsperado)
		toleranceAmount := baseGravada * v.tolerance
		if diff > toleranceAmount {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "itbis_facturado",
				Code:    "itbis_mismatch_exento_isc",
				Message: "ITBIS no coincide con 18% strict (factura tiene exentos/ISC/no facturable, normal)",
			})
		}
		return
	}

	// Skip si categoria ISC = "seguros" — ISC seguros tiene reglas distintas
	if input.ISCCategoria == "seguros" {
		return
	}

	diff := math.Abs(input.ITBISFacturado - itbisEsperado)
	toleranceAmount := baseGravada * v.tolerance

	if diff > toleranceAmount {
		result.Errors = append(result.Errors, ValidationError{
			Field:    "itbis_facturado",
			Code:     "itbis_mismatch",
			Expected: round2(itbisEsperado),
			Actual:   round2(input.ITBISFacturado),
			Message:  "ITBIS no coincide con 18% de base gravada",
		})
	}
}

// validateTotal checks total matches sum of components
func (v *TaxValidator) validateTotal(input *InvoiceInput, result *ValidationResult, totalEsperado float64) {
	if input.TotalFactura <= 0 {
		return
	}

	diff := math.Abs(input.TotalFactura - totalEsperado)
	toleranceAmount := input.TotalFactura * v.tolerance

	if diff > toleranceAmount {
		result.Errors = append(result.Errors, ValidationError{
			Field:    "total_factura",
			Code:     "total_mismatch",
			Expected: round2(totalEsperado),
			Actual:   round2(input.TotalFactura),
			Message:  "Total no coincide con suma de componentes",
		})
	}
}

// validatePropina checks propina is ~10% of base
func (v *TaxValidator) validatePropina(input *InvoiceInput, result *ValidationResult, montoFacturado float64) {
	if input.PropinaLegal <= 0 || montoFacturado <= 0 {
		return
	}

	propinaEsperada := montoFacturado * 0.10
	diff := math.Abs(input.PropinaLegal - propinaEsperada)
	toleranceAmount := propinaEsperada * 0.10 // 10% tolerance for propina

	if diff > toleranceAmount {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "propina_legal",
			Code:    "propina_mismatch",
			Message: "Propina no coincide con 10% del monto facturado",
		})
	}
}

// validateTelecom validates ISC (10%) and CDT (2%) for telecom invoices
func (v *TaxValidator) validateTelecom(input *InvoiceInput, result *ValidationResult, baseGravada float64) {
	if input.ISCCategoria != "telecom" || baseGravada <= 0 {
		return
	}

	// Validate ISC (10%) - tolerancia 15% para ISC
	iscEsperado := baseGravada * 0.10
	diffISC := math.Abs(input.ISCMonto - iscEsperado)
	if diffISC > (iscEsperado * 0.15) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "isc_monto",
			Code:    "isc_telecom_mismatch",
			Message: "ISC telecom debería ser 10% de base gravada",
		})
	}

	// Validate CDT (2%)
	cdtEsperado := baseGravada * 0.02
	diffCDT := math.Abs(input.CDTMonto - cdtEsperado)
	if diffCDT > (cdtEsperado * v.tolerance) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "cdt_monto",
			Code:    "cdt_mismatch",
			Message: "CDT debería ser 2% de base gravada",
		})
	}
}

// validateNCF checks NCF format, type and expiration
func (v *TaxValidator) validateNCF(input *InvoiceInput, result *ValidationResult) {
	if input.NCF == "" {
		return
	}

	// W18: sanitize NCF antes regex check — tolera variaciones OCR (dashes, spaces, lowercase)
	cleanedNCF := sanitizeNCF(input.NCF)

	// Validate format: B or E followed by 10-12 digits
	ncfPattern := regexp.MustCompile(`^[BE][0-9]{10,12}$`)
	if !ncfPattern.MatchString(cleanedNCF) {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "ncf",
			Code:    "ncf_invalid_format",
			Message: "NCF debe comenzar con B o E seguido de 10-12 dígitos",
		})
		return
	}

	// Validate NCF type (first 3 chars after B/E)
	// B01=Crédito Fiscal, B02=Consumidor Final, B04=Nota Crédito,
	// B14=Régimen Especial, B15=Gubernamental, B16=Exportación
	tipoNCF := cleanedNCF[0:3]
	validTypes := map[string]string{
		"B01": "Factura Crédito Fiscal",
		"B02": "Factura Consumidor Final",
		"B04": "Nota de Crédito",
		"B14": "Régimen Especial",
		"B15": "Gubernamental",
		"B16": "Exportación",
		"E31": "Factura Electrónica",
		"E32": "Nota Débito Electrónica",
		"E33": "Nota Crédito Electrónica",
		"E34": "Compras Electrónicas",
		"E41": "Comprobante Compras",
		"E43": "Gastos Menores",
		"E44": "Regímenes Especiales",
		"E45": "Gubernamental",
	}
	if _, valid := validTypes[tipoNCF]; !valid {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "ncf",
			Code:    "ncf_unknown_type",
			Message: "Tipo de NCF no reconocido: " + tipoNCF,
		})
	}

	// Check expiration — W17.6: sanitize ISO RFC3339 to YYYY-MM-DD before parse
	if input.NCFVencimiento != "" {
		vencimiento, err := time.Parse("2006-01-02", sanitizeDate(input.NCFVencimiento))
		if err == nil && time.Now().After(vencimiento) {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "ncf_vencimiento",
				Code:    "ncf_expired",
				Message: "NCF vencido",
			})
		}
	}
}

// validateRetenciones checks retention fields are consistent
func (v *TaxValidator) validateRetenciones(input *InvoiceInput, result *ValidationResult) {
	hasRetention := input.ITBISRetenido > 0 || input.RetencionISRMonto > 0

	// If there are retentions, payment date is required
	if hasRetention && input.FechaPago == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "fecha_pago",
			Code:    "missing_payment_date",
			Message: "Fecha de pago requerida cuando hay retenciones",
		})
	}

	// If ISR retention exists, type is required (1-8) and validate rate
	if input.RetencionISRMonto > 0 {
		if input.RetencionISRTipo < 1 || input.RetencionISRTipo > 8 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "retencion_isr_tipo",
				Code:    "missing_retencion_tipo",
				Message: "Tipo de retención ISR requerido (1-8)",
			})
		} else {
			// Validate ISR rate by type
			v.validateISRRate(input, result)
		}
	}
}

// validateISRRate checks ISR retention matches expected rate by type
func (v *TaxValidator) validateISRRate(input *InvoiceInput, result *ValidationResult) {
	// ISR retention rates by type (DGII)
	// 1=Alquileres (10%), 2=Honorarios (10%), 3=Comisiones (10%)
	// 4=Intereses (10%), 5=Dividendos (10%), 6=Premios (25%)
	// 7=Transferencias (27%), 8=Otros (10%)
	isrRates := map[int]float64{
		1: 0.10, // Alquileres
		2: 0.10, // Honorarios profesionales
		3: 0.10, // Comisiones
		4: 0.10, // Intereses pagados a personas físicas
		5: 0.10, // Dividendos
		6: 0.25, // Premios
		7: 0.27, // Transferencias inmobiliarias
		8: 0.10, // Otros
	}

	expectedRate, exists := isrRates[input.RetencionISRTipo]
	if !exists {
		return
	}

	// Calculate base for ISR (usually subtotal - descuento)
	baseISR := input.MontoServicios + input.MontoBienes - input.Descuento
	if baseISR <= 0 {
		return
	}

	expectedISR := baseISR * expectedRate
	diff := math.Abs(input.RetencionISRMonto - expectedISR)
	toleranceAmount := expectedISR * v.tolerance

	if diff > toleranceAmount && expectedISR > 0 {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "retencion_isr_monto",
			Code:    "isr_rate_mismatch",
			Message: "Retención ISR no coincide con tasa esperada para tipo " + string(rune('0'+input.RetencionISRTipo)),
		})
	}
}

// validateCoherence checks field coherence
func (v *TaxValidator) validateCoherence(input *InvoiceInput, result *ValidationResult) {
	// Check at least one amount exists
	if input.MontoServicios == 0 && input.MontoBienes == 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "monto_servicios",
			Code:    "no_amounts",
			Message: "Debe existir monto de servicios o bienes",
		})
	}

	// Check ITBIS coherence with exento
	if input.ITBISFacturado > 0 && input.ITBISExento > 0 {
		base := input.MontoServicios + input.MontoBienes - input.Descuento
		gravada := base - input.ITBISExento
		if gravada < 0 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "itbis_exento",
				Code:    "itbis_exento_exceeds_base",
				Message: "ITBIS exento excede la base imponible",
			})
		}
	}

	// Validate descuento is not greater than subtotal
	subtotal := input.MontoServicios + input.MontoBienes
	if input.Descuento > subtotal {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "descuento",
			Code:    "descuento_exceeds_subtotal",
			Message: "Descuento excede el subtotal",
		})
	}
}

// validateISCSeguros checks ISC is 16% of base gravada for seguros category
func (v *TaxValidator) validateISCSeguros(input *InvoiceInput, result *ValidationResult, baseGravada float64) {
	if input.ISCCategoria != "seguros" || baseGravada <= 0 {
		return
	}
	iscEsperado := baseGravada * 0.16
	diff := math.Abs(input.ISCMonto - iscEsperado)
	if diff > (iscEsperado * 0.15) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "isc_monto",
			Code:    "isc_seguros_mismatch",
			Message: "ISC seguros debería ser 16% de prima neta",
		})
	}
}

// validateNotaCredito checks that credit notes reference the original invoice NCF
func (v *TaxValidator) validateNotaCredito(input *InvoiceInput, result *ValidationResult) {
	notaCreditoTypes := map[string]bool{"B04": true, "E33": true, "E34": true}
	if !notaCreditoTypes[input.TipoNCF] {
		return
	}
	if input.NCFModifica == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "ncf_modifica",
			Code:    "nota_credito_sin_referencia",
			Message: "Nota de crédito requiere NCF de factura original (ncfModifica)",
		})
	}
}

// validateExportaciones checks that export invoices (B16) do not have ITBIS
func (v *TaxValidator) validateExportaciones(input *InvoiceInput, result *ValidationResult) {
	if input.TipoNCF != "B16" {
		return
	}
	if input.ITBISFacturado > 0 {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "itbis_facturado",
			Code:    "exportacion_con_itbis",
			Message: "Facturas de exportación (B16) no deben tener ITBIS",
		})
	}
}

// validateGubernamentales checks that governmental invoices (B15/E45) have ITBIS exento
func (v *TaxValidator) validateGubernamentales(input *InvoiceInput, result *ValidationResult) {
	gubernamentalTypes := map[string]bool{"B15": true, "E45": true}
	if !gubernamentalTypes[input.TipoNCF] {
		return
	}
	if input.ITBISExento == 0 && input.ITBISFacturado > 0 {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "itbis_exento",
			Code:    "gubernamental_sin_exento",
			Message: "Facturas gubernamentales (B15/E45) generalmente tienen ITBIS exento",
		})
	}
}

// validateITBISRetenidoPorcentaje checks retention percentage is 30 or 100
func (v *TaxValidator) validateITBISRetenidoPorcentaje(input *InvoiceInput, result *ValidationResult) {
	if input.ITBISRetenido <= 0 {
		return
	}
	if input.ITBISRetenidoPorcentaje != 30 && input.ITBISRetenidoPorcentaje != 100 {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "itbis_retenido_porcentaje",
			Code:    "itbis_retenido_porcentaje_invalido",
			Message: "ITBIS retenido debe ser 30% (gran contribuyente) o 100% (retenedor designado)",
		})
	}
}

// validateRNCLength checks RNC emisor digit count.
// W17.5: 8 digits = likely OCR cut a digit → yellow warning (NOT error).
// Valid: 9 digits (RNC empresa) or 11 digits (cédula).
// Invalid: < 8 or > 11 (except 9, 11) → error.
func (v *TaxValidator) validateRNCLength(input *InvoiceInput, result *ValidationResult) {
	rnc := input.RNCEmisor
	if rnc == "" {
		return
	}

	// Count digits only (strip non-digit chars if any)
	digits := 0
	for _, r := range rnc {
		if r >= '0' && r <= '9' {
			digits++
		}
	}

	switch {
	case digits == 9 || digits == 11:
		// Valid — no action
	case digits == 8:
		// Likely OCR cut one digit — yellow warning, not error
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "rnc_emisor",
			Code:    "rnc_8_digitos_posible_corte",
			Message: "RNC con 8 dígitos detectado — verificar manualmente (DGII usa 9 u 11)",
		})
	default:
		// Clearly invalid length
		result.Errors = append(result.Errors, ValidationError{
			Field:   "rnc_emisor",
			Code:    "rnc_longitud_invalida",
			Message: "RNC debe tener 9 dígitos (empresa) u 11 dígitos (cédula)",
		})
	}
}

// round2 rounds to 2 decimal places
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// sanitizeNCF normaliza NCF para tolerar variaciones del OCR:
// - Strip whitespace/tabs/newlines
// - Strip guiones, underscore, puntos
// - Uppercase letra inicial (b → B, e → E)
// Ej: "e32-00367-18526" → "E320036718526"
//
//	"B01 0001 2345" → "B010012345"
//
// W18: aplica antes del regex check en validateNCF.
var ncfSanitizeRegex = regexp.MustCompile(`[\s\-_.]`)

func sanitizeNCF(s string) string {
	cleaned := strings.ToUpper(strings.TrimSpace(s))
	cleaned = ncfSanitizeRegex.ReplaceAllString(cleaned, "")
	return cleaned
}

// sanitizeDate strips ISO 8601 timezone/time suffix, returning only YYYY-MM-DD.
// W17.6: handles inputs like "2026-01-31T00:00:00Z", "2026-01-31T00:00:00-04:00", "2026-01-31".
var dateOnlyRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)

func sanitizeDate(s string) string {
	if m := dateOnlyRegex.FindString(s); m != "" {
		return m
	}
	return strings.TrimSpace(s)
}
