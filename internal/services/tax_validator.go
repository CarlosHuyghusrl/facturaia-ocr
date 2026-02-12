package services

import (
	"math"
	"regexp"
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
	ITBISFacturado       float64 `json:"itbis_facturado"`
	ITBISTasa            float64 `json:"itbis_tasa"`  // 18 (normal) o 16 (zona franca)
	ITBISExento          float64 `json:"itbis_exento"`
	ITBISRetenido        float64 `json:"itbis_retenido"`
	ITBISProporcionalidad float64 `json:"itbis_proporcionalidad"`
	ITBISCosto           float64 `json:"itbis_costo"`

	// ISC
	ISCMonto    float64 `json:"isc_monto"`
	ISCCategoria string  `json:"isc_categoria"`

	// Other taxes
	CDTMonto        float64 `json:"cdt_monto"`
	Cargo911        float64 `json:"cargo_911"`
	PropinaLegal    float64 `json:"propina_legal"`
	OtrosImpuestos  float64 `json:"otros_impuestos"`
	MontoNoFacturable float64 `json:"monto_no_facturable"`

	// ISR retention
	RetencionISRTipo  int     `json:"retencion_isr_tipo"`
	RetencionISRMonto float64 `json:"retencion_isr_monto"`

	// Total
	TotalFactura float64 `json:"total_factura"`

	// NCF
	NCF           string `json:"ncf"`
	NCFVencimiento string `json:"ncf_vencimiento"` // YYYY-MM-DD

	// Payment
	FechaPago string `json:"fecha_pago"` // YYYY-MM-DD
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

	// Set final status
	result.Valid = len(result.Errors) == 0
	result.NeedsReview = len(result.Warnings) > 0

	return result
}

// validateITBIS checks ITBIS matches 18% of base gravada
func (v *TaxValidator) validateITBIS(input *InvoiceInput, result *ValidationResult, baseGravada, itbisEsperado float64) {
	if baseGravada <= 0 {
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

	// Validate ISC (10%)
	iscEsperado := baseGravada * 0.10
	diffISC := math.Abs(input.ISCMonto - iscEsperado)
	if diffISC > (iscEsperado * v.tolerance) {
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

	// Validate format: B or E followed by 10-12 digits
	ncfPattern := regexp.MustCompile(`^[BE][0-9]{10,12}$`)
	if !ncfPattern.MatchString(input.NCF) {
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
	tipoNCF := input.NCF[0:3]
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

	// Check expiration
	if input.NCFVencimiento != "" {
		vencimiento, err := time.Parse("2006-01-02", input.NCFVencimiento)
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

// round2 rounds to 2 decimal places
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
