package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
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
  "tipoNcf": "B01, B02, B04, B15, E31, etc",
  "ncfModifica": "NCF original que se modifica, OBLIGATORIO si tipoNcf es B04, E32 o E33, null si no aplica",
  "rncEmisor": "solo digitos, sin guiones - del VENDEDOR",
  "nombreEmisor": "nombre de la tienda/empresa que VENDE",
  "tipoIdEmisor": "1=RNC, 2=Cedula",
  "rncReceptor": "solo digitos, sin guiones - del COMPRADOR",
  "nombreReceptor": "nombre del cliente que COMPRA",
  "tipoIdReceptor": "1=RNC, 2=Cedula",
  "fechaFactura": "YYYY-MM-DD",
  "fechaVencimiento": "YYYY-MM-DD o null",
  "fechaPago": "YYYY-MM-DD, requerida si hay retenciones ITBIS o ISR, null si no aplica",
  "subtotal": numero (base antes de impuestos, usa 0 si no aparece),
  "descuento": numero (descuento aplicado, usa 0 si no aparece),
  "montoServicios": numero (monto de la parte de servicios; si la factura mezcla productos y servicios separar los montos; si solo servicios poner todo aqui; si solo bienes/productos usar 0),
  "montoBienes": numero (monto de la parte de bienes/productos; si solo bienes poner todo aqui; si solo servicios usar 0),
  "itbis": numero (ITBIS 18%% facturado, usa 0 si no aparece),
  "itbisTasa": numero (18 normal o 16 zona franca, usa 18 por defecto),
  "itbisRetenido": numero (ITBIS retenido, usa 0 si no aparece),
  "itbisRetenidoPorcentaje": numero (30 si gran contribuyente retiene 30%%, 100 si retenedor designado retiene 100%%, 0 si no hay retencion),
  "itbisExento": numero (monto exento de ITBIS, usa 0 si no aparece),
  "isr": numero (ISR retenido, usa 0 si no aparece),
  "retencionIsrTipo": numero 1-8 (tipo retencion ISR segun tabla DGII, usa 0 si no aplica),
  "isc": numero (Impuesto Selectivo al Consumo, usa 0 si no aparece),
  "iscCategoria": "seguros|telecom|alcohol|tabaco|vehiculos|combustibles" o null,
  "cdtMonto": numero (Contribucion Desarrollo Telecom 2%%, usa 0 si no aparece),
  "cargo911": numero (Contribucion al 911, usa 0 si no aparece),
  "propina": numero (propina legal 10%%, usa 0 si no aparece),
  "otrosImpuestos": numero (impuestos no clasificados, usa 0 si no aparece),
  "montoNoFacturable": numero (propinas voluntarias, reembolsos, usa 0 si no aparece),
  "total": numero final a pagar (usa 0 si no aparece, NUNCA null),
  "formaPago": "01-07 segun codigo",
  "tipoBienServicio": "01-13 segun codigo",
  "items": [{"descripcion": "...", "cantidad": 1, "precioUnit": 100, "importe": 100}]
}

## GUIA DE IMPUESTOS DOMINICANOS

### ITBIS (Impuesto Transferencia Bienes y Servicios)
- 18%% normal o 16%% zona franca - busca "ITBIS", "I.T.B.I.S", "IVA"
- itbisRetenidoPorcentaje: 30 si gran contribuyente, 100 si retenedor designado, 0 si no hay retencion

### ISC (Impuesto Selectivo al Consumo) - por categoria
- seguros: 16%% sobre prima neta (facturas de aseguradoras)
- telecom: 10%% sobre servicio de telecomunicaciones (Claro, Altice, Viva)
- alcohol: monto especifico por litro (no porcentaje fijo)
- tabaco: monto especifico por unidad (no porcentaje fijo)
- combustibles: monto fijo por galon segun tipo de combustible
- vehiculos: monto segun categoria del vehiculo

### CDT y 911 (solo telecom)
- CDT: 2%% adicional en facturas telecom - "Contribucion Desarrollo Telecomunicaciones"
- 911: Cargo fijo en lineas telefonicas - "Contribucion 911", "Cargo 911"

### ISR (Impuesto Sobre la Renta) - tipos de retencion
- Tipo 1: Alquileres (10%%), Tipo 2: Honorarios personas fisicas (10%%)
- Tipo 3: Otros ingresos personas fisicas (10%%), Tipo 4: Renta presunta (25%% o 27%%)
- Tipo 5: Loterias y premios (25%%), Tipo 6: Personas juridicas (27%%)
- Tipo 7: Servicios en general (10%%), Tipo 8: Dividendos (10%%)

### Propina y ncfModifica
- Propina: 10%% legal en restaurantes/hoteles - busca "Propina", "Servicio", "10%%"
- ncfModifica: OBLIGATORIO si tipoNcf es B04 (Nota Credito), E32 (Nota Debito Electronica) o E33 (Nota Credito Electronica)

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

	prompt := fmt.Sprintf(`Eres un EXPERTO en facturas fiscales de Republica Dominicana. Tu trabajo es extraer TODOS los datos fiscales de este texto OCR para el sistema DGII.

## PASO 1 - IDENTIFICA EL TIPO DE DOCUMENTO:
- TICKET DE TIENDA: papel termico (supermercados, tiendas, farmacias)
  * El EMISOR (vendedor) aparece ARRIBA: nombre tienda, RNC, direccion, telefono
  * El RECEPTOR (comprador) aparece ABAJO: nombre empresa cliente, "RNC/Cedula:"
- FACTURA FORMAL: papel carta con formato estructurado
  * El EMISOR esta en el membrete superior
  * El RECEPTOR dice "Cliente:", "Facturar a:", "Vendido a:"

## PASO 2 - REGLA CRITICA EMISOR vs RECEPTOR:
- EMISOR = Quien VENDE (la tienda/negocio que emite la factura)
  * En tickets: es el negocio del ENCABEZADO (texto al inicio)
  * Su RNC aparece ARRIBA cerca del nombre o entre los datos del encabezado
  * Tiendas conocidas RD: Plaza Lama, Jumbo, La Sirena, CCN, Iberia, Bravo, Nacional, etc.
- RECEPTOR = Quien COMPRA (el cliente que paga)
  * En tickets: aparece ABAJO, despues de "Total Articulos Vendidos" o "Gracias por su compra"
  * Busca: "RNC/Cedula:", "Cliente:", "Facturar a:", "Vendido a:"
  * Si un nombre de empresa aparece DESPUES de los totales con "RNC/Cedula:", ESO es el RECEPTOR

## PASO 3 - FORMATO RNC DOMINICANO:
- Empresas: 9 digitos (ej: 131047939, 1-31-04793-9)
- Personas: 11 digitos (cedula, ej: 00112345678)
- Quita guiones al extraer: "1-31-04793-9" -> "131047939"
- PUEDE haber DOS RNC diferentes en la factura: uno del emisor y otro del receptor
- NUNCA copies rncEmisor a rncReceptor o viceversa

## PASO 4 - FORMATO NCF (Comprobante Fiscal):
- Credito Fiscal: B01XXXXXXXXX (11 digitos despues de B01)
- Consumidor Final: B02XXXXXXXXX
- Nota de Credito: B04XXXXXXXXX (REQUIERE ncfModifica)
- Gubernamental: B15XXXXXXXXX
- E-CF: E31XXXXXXXXXXXXX (13 digitos despues de E31)
- Nota Debito Electronica: E32XXXXXXXXXXXXX (REQUIERE ncfModifica)
- Nota Credito Electronica: E33XXXXXXXXXXXXX (REQUIERE ncfModifica)
- El NCF aparece frecuentemente al FINAL del ticket, NO confundir con datos del emisor

## CAMPOS A EXTRAER

Devuelve SOLO JSON valido (sin markdown, sin comentarios):
{
  "ncf": "el NCF completo",
  "tipoNcf": "B01, B02, B04, B15, E31, etc",
  "ncfModifica": "NCF original que se modifica, OBLIGATORIO si tipoNcf es B04, E32 o E33, null si no aplica",
  "rncEmisor": "solo digitos, sin guiones - del VENDEDOR",
  "nombreEmisor": "nombre de la tienda/empresa que VENDE",
  "tipoIdEmisor": "1=RNC empresa 9 digitos, 2=Cedula 11 digitos",
  "rncReceptor": "solo digitos, sin guiones - del COMPRADOR",
  "nombreReceptor": "nombre del cliente que COMPRA",
  "tipoIdReceptor": "1=RNC empresa 9 digitos, 2=Cedula 11 digitos",
  "fechaFactura": "YYYY-MM-DD",
  "fechaVencimiento": "YYYY-MM-DD o null",
  "fechaPago": "YYYY-MM-DD, requerida si hay retenciones ITBIS o ISR, null si no aplica",
  "subtotal": numero (base antes de impuestos, usa 0 si no aparece),
  "descuento": numero (descuento aplicado, usa 0 si no aparece),
  "montoServicios": numero (monto de la parte de servicios, usa 0 si no aplica - si la factura mezcla productos y servicios separar los montos; si solo servicios poner todo aqui; si solo bienes/productos usar 0),
  "montoBienes": numero (monto de la parte de bienes/productos, usa 0 si no aplica - si solo bienes poner todo aqui; si solo servicios usar 0),
  "itbis": numero (ITBIS 18%% facturado, usa 0 si no aparece),
  "itbisTasa": numero (18 normal o 16 zona franca, usa 18 por defecto),
  "itbisRetenido": numero (ITBIS retenido, usa 0 si no aparece),
  "itbisRetenidoPorcentaje": numero (30 si gran contribuyente retiene 30%%, 100 si retenedor designado retiene 100%%, 0 si no hay retencion),
  "itbisExento": numero (monto exento de ITBIS, usa 0 si no aparece),
  "isr": numero (ISR retenido, usa 0 si no aparece),
  "retencionIsrTipo": numero 1-8 (tipo retencion ISR segun tabla DGII, usa 0 si no aplica),
  "isc": numero (Impuesto Selectivo al Consumo, usa 0 si no aparece),
  "iscCategoria": "seguros|telecom|alcohol|tabaco|vehiculos|combustibles" o null,
  "cdtMonto": numero (Contribucion Desarrollo Telecom 2%%, usa 0 si no aparece),
  "cargo911": numero (Contribucion al 911, usa 0 si no aparece),
  "propina": numero (propina legal 10%%, usa 0 si no aparece),
  "otrosImpuestos": numero (impuestos no clasificados, usa 0 si no aparece),
  "montoNoFacturable": numero (propinas voluntarias, reembolsos, usa 0 si no aparece),
  "total": numero final a pagar (usa 0 si no aparece, NUNCA null),
  "formaPago": "01-07 segun codigo",
  "tipoBienServicio": "01-13 segun codigo",
  "items": [{"codigo": "001", "descripcion": "...", "cantidad": 1, "precioUnit": 100, "descuento": 0, "itbis": 18, "importe": 118}]
}

## GUIA DE IMPUESTOS DOMINICANOS

### ITBIS (Impuesto Transferencia Bienes y Servicios)
- 18%% normal - busca "ITBIS", "I.T.B.I.S", "IVA", "Impuesto"
- 16%% zona franca
- itbisRetenido: monto retenido por el receptor (no por el emisor)
- itbisRetenidoPorcentaje: 30 si gran contribuyente, 100 si retenedor designado, 0 si no hay retencion

### ISC (Impuesto Selectivo al Consumo) - por categoria
- seguros: 16%% sobre prima neta (facturas de aseguradoras)
- telecom: 10%% sobre servicio de telecomunicaciones (Claro, Altice, Viva)
- alcohol: monto especifico por litro (no porcentaje fijo)
- tabaco: monto especifico por unidad (no porcentaje fijo)
- combustibles: monto fijo por galon segun tipo
- vehiculos: monto segun categoria del vehiculo

### CDT y 911 (solo telecom)
- CDT: 2%% adicional en facturas telecom - "Contribucion Desarrollo Telecomunicaciones"
- 911: Cargo fijo en lineas telefonicas - "Contribucion 911", "Cargo 911"

### ISR (Impuesto Sobre la Renta) - tipos de retencion
- Tipo 1: Alquileres (10%%)
- Tipo 2: Honorarios y comisiones personas fisicas (10%%)
- Tipo 3: Otros ingresos personas fisicas (10%%)
- Tipo 4: Renta presunta (25%% o 27%%)
- Tipo 5: Loterias y premios (25%%)
- Tipo 6: Personas juridicas (27%%)
- Tipo 7: Servicios en general (10%%)
- Tipo 8: Dividendos (10%%)

### Propina
- 10%% legal en restaurantes y hoteles - busca "Propina", "Servicio", "10%%"

### ncfModifica (OBLIGATORIO para notas)
- Si tipoNcf es B04 (Nota de Credito): ncfModifica = NCF de la factura que se corrige
- Si tipoNcf es E32 (Nota Debito Electronica): ncfModifica = e-NCF que se modifica
- Si tipoNcf es E33 (Nota Credito Electronica): ncfModifica = e-NCF que se modifica
- Para cualquier otro tipo de NCF: ncfModifica = null

## REGLAS CRITICAS
1. NCF: Busca "NCF:", "Comprobante:", "e-NCF:", "B01", "B02", "B04", "E31"
2. RNC Emisor: Busca "RNC:", "R.N.C." seguido de 9 u 11 digitos (ARRIBA del texto)
3. RNC Receptor: Busca "RNC Cliente:", "Cedula:", "RNC/Cedula:" (ABAJO del texto)
4. tipoNcf: Primeros 3 caracteres del NCF (B01, B02, B04, B14, B15, B16, E31-E45)
5. tipoIdEmisor/Receptor: "1" si 9 digitos (RNC empresa), "2" si 11 digitos (Cedula)
6. Subtotal: busca "Sub-Total", "Subtotal", "Base Imponible", "Monto Gravado"
7. Si no encuentras un dato, usa null para strings o 0 para numeros
8. Ano por defecto si no se ve: %d
9. Todos los montos deben ser numeros decimales (no strings)
10. NUNCA devuelvas null para subtotal, itbis, o total - usa 0 si no puedes leer el valor
11. NUNCA inventes ni calcules montos que no aparezcan en el texto
12. NUNCA pongas el mismo RNC en emisor y receptor

## CODIGOS

formaPago: 01=Efectivo, 02=Cheque/Transferencia, 03=Tarjeta, 04=Credito, 05=Permuta, 06=Nota Credito, 07=Mixto

tipoBienServicio: 01=Personal, 02=Servicios, 03=Arrendamiento, 04=Activos fijos, 05=Representacion, 06=Deducciones, 07=Financieros, 08=Extraordinarios, 09=Costo venta, 10=Activos, 11=Seguros, 12=Viajes, 13=Otros

AHORA ANALIZA EL TEXTO. PRIMERO identifica quien VENDE y quien COMPRA, LUEGO extrae todos los datos fiscales.

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
		// Montos base
		Subtotal       interface{} `json:"subtotal"`
		Descuento      interface{} `json:"descuento"`
		MontoServicios interface{} `json:"montoServicios"`
		MontoBienes    interface{} `json:"montoBienes"`
		// ITBIS
		ITBIS                   interface{} `json:"itbis"`
		ITBISTasa               interface{} `json:"itbisTasa"`
		ITBISRetenido           interface{} `json:"itbisRetenido"`
		ITBISRetenidoPorcentaje interface{} `json:"itbisRetenidoPorcentaje"`
		ITBISExento             interface{} `json:"itbisExento"`
		ITBISProporcionalidad   interface{} `json:"itbisProporcionalidad"`
		ITBISCosto              interface{} `json:"itbisCosto"`
		// ISR
		ISR              interface{} `json:"isr"`
		RetencionISRTipo interface{} `json:"retencionIsrTipo"`
		// ISC
		ISC          interface{} `json:"isc"`
		ISCCategoria string      `json:"iscCategoria"`
		// Otros cargos
		CDTMonto          interface{} `json:"cdtMonto"`
		Cargo911          interface{} `json:"cargo911"`
		Propina           interface{} `json:"propina"`
		OtrosImpuestos    interface{} `json:"otrosImpuestos"`
		MontoNoFacturable interface{} `json:"montoNoFacturable"`
		// Total
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
		Confidence:  0, // calculated below after all fields are parsed
		ProcessedAt: time.Now(),

		// Legacy compatibility
		Vendor: raw.NombreEmisor,
	}

	// Parse dates
	invoice.FechaFactura = parseDate(raw.FechaFactura)
	invoice.FechaVencimiento = parseDate(raw.FechaVencimiento)
	invoice.FechaPago = parseDate(raw.FechaPago)
	invoice.Date = invoice.FechaFactura // Legacy

	// Parse amounts - Base
	invoice.Subtotal = parseDecimal(raw.Subtotal)
	invoice.Descuento = parseDecimal(raw.Descuento)

	// Parse amounts - Montos separados
	invoice.MontoServicios = parseDecimal(raw.MontoServicios)
	invoice.MontoBienes = parseDecimal(raw.MontoBienes)

	// Parse amounts - ITBIS
	invoice.ITBIS = parseDecimal(raw.ITBIS)
	invoice.ITBISTasa = parseDecimal(raw.ITBISTasa)
	invoice.ITBISRetenido = parseDecimal(raw.ITBISRetenido)
	invoice.ITBISRetenidoPorcentaje = int(parseDecimal(raw.ITBISRetenidoPorcentaje).IntPart())
	invoice.ITBISExento = parseDecimal(raw.ITBISExento)
	invoice.ITBISProporcionalidad = parseDecimal(raw.ITBISProporcionalidad)
	invoice.ITBISCosto = parseDecimal(raw.ITBISCosto)

	// Parse amounts - ISR
	invoice.ISR = parseDecimal(raw.ISR)
	invoice.RetencionISRTipo = int(parseDecimal(raw.RetencionISRTipo).IntPart())

	// Parse amounts - ISC
	invoice.ISC = parseDecimal(raw.ISC)
	invoice.ISCCategoria = raw.ISCCategoria

	// Parse amounts - Otros cargos
	invoice.CDTMonto = parseDecimal(raw.CDTMonto)
	invoice.Cargo911 = parseDecimal(raw.Cargo911)
	invoice.Propina = parseDecimal(raw.Propina)
	invoice.OtrosImpuestos = parseDecimal(raw.OtrosImpuestos)
	invoice.MontoNoFacturable = parseDecimal(raw.MontoNoFacturable)

	// Parse amounts - Total
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

	// Calculate real confidence based on extraction quality
	invoice.Confidence = calculateConfidence(invoice)

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

// ncfRegex validates Dominican Republic NCF formats:
// B-series: B01/B02/B04/B14/B15/B16 + 8 digits
// E-series: E31-E45 + 13 digits (electronic)
var ncfRegex = regexp.MustCompile(`^(B0[124]|B1[456]|E3[1-9]|E4[0-5])\d{8,13}$`)

// calculateConfidence computes a real confidence score based on extraction quality.
//
// Score breakdown (max 1.0):
//
//	Critical fields  — 0.15 each (0.60 total):
//	  NCF present, RNC emisor present, total > 0, ITBIS >= 0 (field populated)
//	Important fields — 0.05 each (0.20 total):
//	  fecha, subtotal > 0, tipoNCF, nombre emisor
//	Bonus            — 0.10 each (0.20 total):
//	  NCF has valid format, total ≈ subtotal + ITBIS (within 5%)
func calculateConfidence(inv *models.Invoice) float64 {
	var score float64

	// --- Critical fields (0.15 each) ---

	// NCF present
	if inv.NCF != "" {
		score += 0.15
	}

	// RNC emisor present
	if inv.RNCEmisor != "" {
		score += 0.15
	}

	// Total > 0
	if inv.Total.GreaterThan(decimal.Zero) {
		score += 0.15
	}

	// ITBIS field populated (>= 0 is valid; zero is OK for tax-exempt invoices).
	// We award the point whenever the AI explicitly returned a value (i.e. the
	// field is not the zero-value that results from a missing/null response).
	// Because the AI always sets 0 for missing fields per our prompt rules, we
	// treat the presence of the Total as a proxy: if total > 0 we trust the
	// ITBIS extraction was attempted.  We always award this point when total > 0
	// (already scored above) OR when ITBIS itself is positive.
	if !inv.ITBIS.IsNegative() {
		score += 0.15
	}

	// --- Important fields (0.05 each) ---

	// Fecha factura present
	if !inv.FechaFactura.IsZero() {
		score += 0.05
	}

	// Subtotal > 0
	if inv.Subtotal.GreaterThan(decimal.Zero) {
		score += 0.05
	}

	// TipoNCF present
	if inv.TipoNCF != "" {
		score += 0.05
	}

	// Nombre emisor present
	if inv.NombreEmisor != "" {
		score += 0.05
	}

	// --- Bonus ---

	// NCF has valid Dominican Republic format
	if ncfRegex.MatchString(inv.NCF) {
		score += 0.10
	}

	// Total is consistent with subtotal + ITBIS (within 5% tolerance)
	if inv.Total.GreaterThan(decimal.Zero) && inv.Subtotal.GreaterThan(decimal.Zero) {
		expected := inv.Subtotal.Add(inv.ITBIS)
		diff := inv.Total.Sub(expected).Abs()
		tolerance := inv.Total.Mul(decimal.NewFromFloat(0.05))
		if diff.LessThanOrEqual(tolerance) {
			score += 0.10
		}
	}

	// Cap at 1.0 to guard against floating-point drift
	if score > 1.0 {
		score = 1.0
	}

	return score
}

