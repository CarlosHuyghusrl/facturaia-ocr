package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/facturaIA/invoice-ocr-service/internal/models"
	"github.com/shopspring/decimal"
)

// Extractor handles AI-based data extraction from OCR text or images
type Extractor struct {
	provider   Provider
	categories []string
}

// NewExtractor creates a new AI extractor
func NewExtractor(provider Provider, categories []string) *Extractor {
	return &Extractor{
		provider:   provider,
		categories: categories,
	}
}

// Extract processes OCR text or image and returns structured invoice data
func (e *Extractor) Extract(ocrText string, imageBase64 string) (*models.Invoice, float64, error) {
	startTime := time.Now()

	// Determine if we are using vision mode (image) or text mode (OCR)
	isVisionMode := imageBase64 != "" && strings.TrimSpace(ocrText) == ""

	// Build appropriate prompt
	var prompt string
	if isVisionMode {
		prompt = e.buildPromptVision()
	} else {
		prompt = e.buildPromptDGII(ocrText)
	}

	// Call AI provider
	response, err := e.provider.ExtractData(prompt, imageBase64)
	if err != nil {
		return nil, 0, fmt.Errorf("AI extraction failed: %w", err)
	}

	duration := time.Since(startTime).Seconds()

	// Log AI response for debugging
	fmt.Printf("[AI Response] Vision mode: %v, Response length: %d\n", isVisionMode, len(response))
	fmt.Printf("[AI Response] Raw: %s\n", response)

	// Parse JSON response
	invoice, err := e.parseResponseDGII(response, ocrText)
	if err != nil {
		return nil, duration, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return invoice, duration, nil
}

// buildPromptVision creates prompt for direct image analysis (Gemini Vision)
func (e *Extractor) buildPromptVision() string {
	currentYear := time.Now().Year()

	prompt := fmt.Sprintf(`Eres un EXPERTO en OCR y facturas fiscales de Republica Dominicana. Tu trabajo es LEER CUIDADOSAMENTE cada caracter de la imagen.

## INSTRUCCIONES DE LECTURA

PASO 1 - EXAMINA TODA LA IMAGEN COMPLETA:
- Mira PRIMERO el encabezado arriba (logo, nombre empresa grande, direccion, telefono)
- Mira la parte central (items, precios, totales)
- Mira la parte inferior (datos del comprador, e-NCF, codigo de barras)
- Mira los sellos/timbres (pueden tener RNC dentro de un circulo)

PASO 2 - IDENTIFICA EL TIPO DE DOCUMENTO:
- TICKET DE TIENDA: Papel termico largo y angosto (supermercados, tiendas, farmacias)
  * El EMISOR (vendedor) esta ARRIBA: logo, nombre tienda, RNC, direccion, telefono
  * El RECEPTOR (comprador) esta ABAJO: nombre empresa, "RNC/Cedula:", telefono
  * Pistas del emisor: URL web (www.xxx.com), "CLUB xxx", slogan, logo
- FACTURA FORMAL: Papel carta con formato estructurado
  * El EMISOR esta en el membrete superior
  * El RECEPTOR dice "Cliente:", "Facturar a:", "Vendido a:"

PASO 3 - REGLA CRITICA EMISOR vs RECEPTOR:
- EMISOR = Quien VENDE (la tienda/negocio que emite la factura)
  * En tickets: es el negocio del ENCABEZADO (parte superior)
  * Su RNC aparece ARRIBA cerca del logo o en un sello
  * Si ves "www.xxx.com" o "CLUB xxx", el nombre de esa empresa es el EMISOR
  * Tiendas conocidas RD: Plaza Lama, Jumbo, La Sirena, CCN, Iberia, Bravo, Nacional, etc.
- RECEPTOR = Quien COMPRA (el cliente que paga)
  * En tickets: aparece ABAJO, despues de "Total Articulos Vendidos" o "Gracias por su compra"
  * Busca: "RNC/Cedula:", "Cliente:", "Facturar a:", "Vendido a:"
  * Si un nombre de empresa aparece DEBAJO de los totales con "RNC/Cedula:", ESO es el RECEPTOR

PASO 4 - EJEMPLO DE TICKET DOMINICANO TIPICO:
  [Logo/Nombre Tienda]     <-- ESTO ES EL EMISOR
  [RNC: XXXXXXXXX]         <-- rncEmisor
  [Direccion, Tel]
  ---
  [Items y precios]
  [ITBIS: X,XXX.XX]
  [TOTAL: XX,XXX.XX]
  ---
  [Total Articulos Vendidos]
  [Nombre Empresa Cliente]  <-- ESTO ES EL RECEPTOR
  [RNC/Cedula: XXXXXXXXX]   <-- rncReceptor
  [e-NCF: EXXXXXXXXXX]

## FORMATO RNC DOMINICANO
- Empresas: 9 digitos (ej: 131047939, 1-31-04793-9)
- Personas: 11 digitos (cedula, ej: 00112345678)
- Quita guiones al extraer: "1-31-04793-9" -> "131047939"
- PUEDE haber DOS RNC diferentes en la factura: uno del emisor y otro del receptor

## FORMATO NCF (Comprobante Fiscal)
- Credito Fiscal: B01XXXXXXXXX (11 digitos despues de B01)
- Consumidor Final: B02XXXXXXXXX
- Gubernamental: B15XXXXXXXXX
- E-CF: E31XXXXXXXXXXXXX (13 digitos despues de E31)
- El e-NCF aparece frecuentemente al FINAL del ticket, NO confundir con datos del emisor

## CAMPOS A EXTRAER

Devuelve SOLO JSON valido (sin markdown, sin comentarios):
{
  "ncf": "el NCF completo",
  "tipoNcf": "B01, B02, B15, E31, etc",
  "rncEmisor": "solo digitos, sin guiones - del VENDEDOR",
  "nombreEmisor": "nombre de la tienda/empresa que VENDE",
  "tipoIdEmisor": "1=RNC, 2=Cedula",
  "rncReceptor": "solo digitos, sin guiones - del COMPRADOR",
  "nombreReceptor": "nombre del cliente que COMPRA",
  "tipoIdReceptor": "1=RNC, 2=Cedula",
  "fechaFactura": "YYYY-MM-DD",
  "fechaVencimiento": "YYYY-MM-DD o null",
  "subtotal": numero sin comas (usa 0 si no aparece, NUNCA null),
  "itbis": numero (impuesto 18%%, usa 0 si no aparece, NUNCA null),
  "itbisRetenido": numero (usa 0 si no aparece, NUNCA null),
  "propina": numero (usa 0 si no aparece, NUNCA null),
  "otrosImpuestos": numero (usa 0 si no aparece, NUNCA null),
  "total": numero final a pagar (usa 0 si no aparece, NUNCA null),
  "formaPago": "01-07 segun codigo",
  "tipoBienServicio": "01-13 segun codigo",
  "items": [{"descripcion": "...", "cantidad": 1, "precioUnit": 100, "importe": 100}]
}

## REGLAS CRITICAS

1. LEE CARACTER POR CARACTER si el texto es dificil
2. Los SELLOS tienen informacion importante - no los ignores
3. Si ves un RNC en un sello circular, ESE es el rncEmisor
4. NUNCA inventes datos - usa null si no puedes leer
5. NUNCA copies rncEmisor a rncReceptor o viceversa
6. NUNCA pongas el mismo RNC en emisor y receptor
7. Si ves "RNC/Cedula:" DEBAJO de los totales, ESE es el RECEPTOR (comprador)
8. Si ves un nombre de empresa con URL web, ESA empresa es el EMISOR (vendedor)
9. El TOTAL siempre es el numero MAS GRANDE al final
10. Si el encabezado esta borroso, busca pistas: URL web, "CLUB xxx", slogan, direccion
11. Ano por defecto si no se ve: %d
12. NUNCA devuelvas null para subtotal, itbis, o total - usa 0 si no puedes leer el valor
13. NUNCA inventes ni calcules montos que no puedas leer en la imagen
14. Si un campo numerico no aparece en la factura, pon 0 (no null, no calculado)

## CODIGOS

formaPago: 01=Efectivo, 02=Cheque/Transferencia, 03=Tarjeta, 04=Credito, 05=Permuta, 06=Nota Credito, 07=Mixto

tipoBienServicio: 01=Personal, 02=Servicios, 03=Arrendamiento, 04=Activos fijos, 05=Representacion, 06=Deducciones, 07=Financieros, 08=Extraordinarios, 09=Costo venta, 10=Activos, 11=Seguros, 12=Viajes, 13=Otros

AHORA ANALIZA LA IMAGEN CUIDADOSAMENTE. PRIMERO identifica quien VENDE y quien COMPRA, LUEGO extrae los datos.`, currentYear)

	return prompt
}
// buildPromptDGII creates specialized prompt for Dominican Republic invoices (OCR text mode)
func (e *Extractor) buildPromptDGII(ocrText string) string {
	currentYear := time.Now().Year()

	prompt := fmt.Sprintf(`Eres un experto en facturas fiscales de Republica Dominicana. Extrae TODOS los datos de esta factura para el sistema DGII.

IMPORTANTE: Las facturas dominicanas tienen:
- NCF (Numero de Comprobante Fiscal): formato B0100000001, E310000000001, etc.
- RNC: 9 digitos (empresas) o 11 digitos (personas/cedula)
- ITBIS: Impuesto del 18%% sobre productos gravados

Extrae y devuelve SOLO JSON valido con esta estructura exacta (sin markdown, sin comentarios):
{
  "ncf": "B0100000001",
  "tipoNcf": "B01",
  "rncEmisor": "123456789",
  "nombreEmisor": "Nombre del negocio",
  "tipoIdEmisor": "1",
  "rncReceptor": "987654321",
  "nombreReceptor": "Cliente",
  "tipoIdReceptor": "1",
  "fechaFactura": "YYYY-MM-DD",
  "fechaVencimiento": "YYYY-MM-DD",
  "subtotal": 1000.00,
  "itbis": 180.00,
  "itbisRetenido": 0.00,
  "propina": 0.00,
  "otrosImpuestos": 0.00,
  "total": 1180.00,
  "formaPago": "01",
  "tipoBienServicio": "02",
  "items": [
    {
      "codigo": "001",
      "descripcion": "Producto o servicio",
      "cantidad": 1,
      "precioUnit": 100.00,
      "descuento": 0.00,
      "itbis": 18.00,
      "importe": 118.00
    }
  ]
}

REGLAS DE EXTRACCION:
1. NCF: Busca patrones como "NCF:", "Comprobante:", "B01", "B02", "B04", "B14", "B15", "E31"
2. RNC Emisor: Busca "RNC:", "R.N.C.", seguido de 9 u 11 digitos
3. RNC Receptor: Puede estar como "RNC Cliente:", "Cedula:", "Cliente RNC:"
4. tipoNcf: Extrae los primeros 3 caracteres del NCF (B01, B02, B04, B14, B15, B16, E31)
5. tipoIdEmisor/Receptor: "1" si tiene 9 digitos (RNC), "2" si tiene 11 (Cedula)
6. Subtotal: Monto antes de ITBIS (puede decir "Sub-Total", "Subtotal", "Base Imponible")
7. ITBIS: 18%% - busca "ITBIS", "I.T.B.I.S", "IVA", "Impuesto"
8. Propina: 10%% legal - busca "Propina", "Servicio", "10%%"
9. formaPago: "01"=Efectivo, "02"=Cheque/Transferencia, "03"=Tarjeta, "04"=Credito
10. tipoBienServicio: Clasifica segun el contenido (ver codigos abajo)
11. Si no encuentras un dato, usa null o string vacio
12. Ano por defecto: %d
13. Todos los montos deben ser numeros decimales (no strings)

CODIGOS tipoBienServicio:
01=Personal, 02=Servicios, 03=Arrendamiento, 04=Activos fijos, 05=Representacion,
06=Deducciones, 07=Financieros, 08=Extraordinarios, 09=Costo venta, 10=Activos,
11=Seguros, 12=Viajes, 13=Otros

CODIGOS formaPago:
01=Efectivo, 02=Cheque/Transferencia, 03=Tarjeta, 04=Credito, 05=Permuta, 06=Nota Credito, 07=Mixto

Texto de la factura:
%s`, currentYear, ocrText)

	return prompt
}
// parseResponseDGII converts AI JSON response to Invoice struct with DGII fields
func (e *Extractor) parseResponseDGII(response string, ocrText string) (*models.Invoice, error) {
	// Clean response (remove markdown code blocks if present)
	cleaned := strings.TrimSpace(response)
	backticks := string([]byte{96, 96, 96})
	cleaned = strings.ReplaceAll(cleaned, backticks+"json", "")
	cleaned = strings.ReplaceAll(cleaned, backticks, "")
	cleaned = strings.TrimSpace(cleaned)

	// Parse JSON - use interface{} for flexible number parsing (handles strings with commas)
	var raw struct {
		NCF              string      `json:"ncf"`
		TipoNCF          string      `json:"tipoNcf"`
		NCFModifica      string      `json:"ncfModifica"`
		RNCEmisor        string      `json:"rncEmisor"`
		NombreEmisor     string      `json:"nombreEmisor"`
		TipoIDEmisor     string      `json:"tipoIdEmisor"`
		RNCReceptor      string      `json:"rncReceptor"`
		NombreReceptor   string      `json:"nombreReceptor"`
		TipoIDReceptor   string      `json:"tipoIdReceptor"`
		FechaFactura     string      `json:"fechaFactura"`
		FechaVencimiento string      `json:"fechaVencimiento"`
		FechaPago        string      `json:"fechaPago"`
		Subtotal         interface{} `json:"subtotal"`
		ITBIS            interface{} `json:"itbis"`
		ITBISRetenido    interface{} `json:"itbisRetenido"`
		ISR              interface{} `json:"isr"`
		Propina          interface{} `json:"propina"`
		OtrosImpuestos   interface{} `json:"otrosImpuestos"`
		Total            interface{} `json:"total"`
		FormaPago        string      `json:"formaPago"`
		TipoBienServicio string      `json:"tipoBienServicio"`
		Items            []struct {
			Codigo         string      `json:"codigo"`
			Descripcion    string      `json:"descripcion"`
			Cantidad       interface{} `json:"cantidad"`
			PrecioUnit     interface{} `json:"precioUnit"`
			PrecioUnitario interface{} `json:"precioUnitario"` // Alternative field name
			Descuento      interface{} `json:"descuento"`
			ITBIS          interface{} `json:"itbis"`
			Importe        interface{} `json:"importe"`
			MontoTotal     interface{} `json:"montoTotal"` // Alternative field name
		} `json:"items"`
	}

	err := json.Unmarshal([]byte(cleaned), &raw)
	if err != nil {
		return nil, fmt.Errorf("JSON parse error: %w - Response: %s", err, cleaned)
	}

	// Build invoice with all DGII fields
	invoice := &models.Invoice{
		// Comprobante fiscal
		NCF:         cleanNCF(raw.NCF),
		TipoNCF:     raw.TipoNCF,
		NCFModifica: raw.NCFModifica,

		// Emisor
		RNCEmisor:    cleanRNC(raw.RNCEmisor),
		NombreEmisor: raw.NombreEmisor,
		TipoIDEmisor: raw.TipoIDEmisor,

		// Receptor
		RNCReceptor:    cleanRNC(raw.RNCReceptor),
		NombreReceptor: raw.NombreReceptor,
		TipoIDReceptor: raw.TipoIDReceptor,

		// Clasificacion
		FormaPago:        raw.FormaPago,
		TipoBienServicio: raw.TipoBienServicio,

		// Metadata
		RawText:     ocrText,
		Confidence:  0.85,
		ProcessedAt: time.Now(),

		// Legacy compatibility
		Vendor: raw.NombreEmisor,
	}

	// Parse dates
	invoice.FechaFactura = parseDate(raw.FechaFactura)
	invoice.FechaVencimiento = parseDate(raw.FechaVencimiento)
	invoice.FechaPago = parseDate(raw.FechaPago)
	invoice.Date = invoice.FechaFactura // Legacy

	// Parse amounts
	invoice.Subtotal = parseDecimal(raw.Subtotal)
	invoice.ITBIS = parseDecimal(raw.ITBIS)
	invoice.ITBISRetenido = parseDecimal(raw.ITBISRetenido)
	invoice.ISR = parseDecimal(raw.ISR)
	invoice.Propina = parseDecimal(raw.Propina)
	invoice.OtrosImpuestos = parseDecimal(raw.OtrosImpuestos)
	invoice.Total = parseDecimal(raw.Total)
	invoice.Tax = invoice.ITBIS // Legacy

	// Determine invoice type
	if invoice.TipoNCF == "B01" || invoice.TipoNCF == "B15" || invoice.TipoNCF == "B14" {
		invoice.TipoFactura = "gastos" // 606 - Compras
	} else if invoice.TipoNCF == "B02" {
		invoice.TipoFactura = "ingresos" // 607 - Ventas (si es consumidor final)
	} else {
		invoice.TipoFactura = "gastos" // Default
	}

	// Parse items
	invoice.Items = make([]models.InvoiceItem, len(raw.Items))
	for i, item := range raw.Items {
		cantidad := parseDecimal(item.Cantidad)
		cantidadInt := 1
		if !cantidad.IsZero() {
			cantidadInt = int(cantidad.IntPart())
		}

		invoice.Items[i] = models.InvoiceItem{
			Codigo:      item.Codigo,
			Descripcion: item.Descripcion,
			Cantidad:    cantidad,
			PrecioUnit:  parseDecimal(item.PrecioUnit),
			Descuento:   parseDecimal(item.Descuento),
			ITBIS:       parseDecimal(item.ITBIS),
			Importe:     parseDecimal(item.Importe),
			// Legacy
			Name:     item.Descripcion,
			Amount:   parseDecimal(item.Importe),
			IsTaxed:  !parseDecimal(item.ITBIS).IsZero(),
			Quantity: cantidadInt,
		}
	}

	// Auto-detect tipo ID if not set
	if invoice.TipoIDEmisor == "" && invoice.RNCEmisor != "" {
		invoice.TipoIDEmisor = detectTipoID(invoice.RNCEmisor)
	}
	if invoice.TipoIDReceptor == "" && invoice.RNCReceptor != "" {
		invoice.TipoIDReceptor = detectTipoID(invoice.RNCReceptor)
	}

	// Auto-extract tipoNcf if not set
	if invoice.TipoNCF == "" && invoice.NCF != "" && len(invoice.NCF) >= 3 {
		invoice.TipoNCF = invoice.NCF[:3]
	}

	return invoice, nil
}

// Helper functions

func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		"2006-01-02",
		"02/01/2006",
		"02-01-2006",
		"2006/01/02",
		"01/02/2006",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseDecimal handles flexible number parsing from interface{}
// Supports: numbers, strings, strings with commas (e.g., "3,965.34")
func parseDecimal(v interface{}) decimal.Decimal {
	if v == nil {
		return decimal.Zero
	}

	switch val := v.(type) {
	case float64:
		return decimal.NewFromFloat(val)
	case int:
		return decimal.NewFromInt(int64(val))
	case int64:
		return decimal.NewFromInt(val)
	case string:
		if val == "" {
			return decimal.Zero
		}
		// Remove commas (thousands separator)
		cleaned := strings.ReplaceAll(val, ",", "")
		d, err := decimal.NewFromString(cleaned)
		if err != nil {
			return decimal.Zero
		}
		return d
	case json.Number:
		if val == "" {
			return decimal.Zero
		}
		d, err := decimal.NewFromString(string(val))
		if err != nil {
			return decimal.Zero
		}
		return d
	default:
		return decimal.Zero
	}
}

func cleanRNC(rnc string) string {
	// Remove non-digits
	var result strings.Builder
	for _, r := range rnc {
		if r >= '0' && r <= '9' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func cleanNCF(ncf string) string {
	// Keep alphanumeric only
	var result strings.Builder
	for _, r := range ncf {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			result.WriteRune(r)
		}
	}
	return strings.ToUpper(result.String())
}

func detectTipoID(id string) string {
	cleaned := cleanRNC(id)
	if len(cleaned) == 9 {
		return "1" // RNC
	} else if len(cleaned) == 11 {
		return "2" // Cedula
	}
	return ""
}

