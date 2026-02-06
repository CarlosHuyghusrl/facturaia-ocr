package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// Invoice represents the extracted data from a receipt/invoice with DGII fields
type Invoice struct {
	// DGII - Comprobante Fiscal
	NCF         string `json:"ncf,omitempty"`         // Numero de Comprobante Fiscal
	TipoNCF     string `json:"tipoNcf,omitempty"`     // Tipo de NCF (B01, B02, B04, B14, B15, E31)
	NCFModifica string `json:"ncfModifica,omitempty"` // NCF que modifica (para notas credito)

	// DGII - Emisor
	RNCEmisor    string `json:"rncEmisor,omitempty"`    // RNC del emisor
	NombreEmisor string `json:"nombreEmisor,omitempty"` // Nombre del emisor
	TipoIDEmisor string `json:"tipoIdEmisor,omitempty"` // 1=RNC, 2=Cedula

	// DGII - Receptor
	RNCReceptor    string `json:"rncReceptor,omitempty"`    // RNC del receptor
	NombreReceptor string `json:"nombreReceptor,omitempty"` // Nombre del receptor
	TipoIDReceptor string `json:"tipoIdReceptor,omitempty"` // 1=RNC, 2=Cedula

	// DGII - Fechas
	FechaFactura     time.Time `json:"fechaFactura,omitempty"`     // Fecha de la factura
	FechaVencimiento time.Time `json:"fechaVencimiento,omitempty"` // Fecha de vencimiento
	FechaPago        time.Time `json:"fechaPago,omitempty"`        // Fecha de pago

	// DGII - Montos
	Subtotal       decimal.Decimal `json:"subtotal,omitempty"`       // Subtotal antes de impuestos
	ITBIS          decimal.Decimal `json:"itbis,omitempty"`          // ITBIS (18%)
	ITBISRetenido  decimal.Decimal `json:"itbisRetenido,omitempty"`  // ITBIS retenido
	ISR            decimal.Decimal `json:"isr,omitempty"`            // ISR retenido
	Propina        decimal.Decimal `json:"propina,omitempty"`        // Propina legal (10%)
	OtrosImpuestos decimal.Decimal `json:"otrosImpuestos,omitempty"` // Otros impuestos

	// DGII - Clasificacion
	FormaPago        string `json:"formaPago,omitempty"`        // 01=Efectivo, 02=Cheque, 03=Tarjeta, etc.
	TipoBienServicio string `json:"tipoBienServicio,omitempty"` // Codigo de bien/servicio
	TipoFactura      string `json:"tipoFactura,omitempty"`      // gastos o ingresos

	// Legacy fields (for backwards compatibility)
	Vendor string          `json:"vendor"`           // Merchant/store name (same as NombreEmisor)
	Date   time.Time       `json:"date"`             // Invoice date (same as FechaFactura)
	Total  decimal.Decimal `json:"total"`            // Total amount
	Tax    decimal.Decimal `json:"tax,omitempty"`    // Tax amount (same as ITBIS)

	// Line items
	Items []InvoiceItem `json:"items,omitempty"` // Individual line items

	// Categories (optional)
	Categories []string `json:"categories,omitempty"` // Suggested categories

	// Raw data
	RawText string `json:"rawText,omitempty"` // Complete OCR text

	// Metadata
	Confidence  float64   `json:"confidence"`   // Overall confidence score (0-1)
	ProcessedAt time.Time `json:"processedAt"`  // When it was processed
}

// InvoiceItem represents a line item in an invoice with DGII fields
type InvoiceItem struct {
	// DGII fields
	Codigo      string          `json:"codigo,omitempty"`      // Codigo del item
	Descripcion string          `json:"descripcion,omitempty"` // Descripcion del item
	Cantidad    decimal.Decimal `json:"cantidad,omitempty"`    // Cantidad
	PrecioUnit  decimal.Decimal `json:"precioUnit,omitempty"`  // Precio unitario
	Descuento   decimal.Decimal `json:"descuento,omitempty"`   // Descuento aplicado
	ITBIS       decimal.Decimal `json:"itbis,omitempty"`       // ITBIS del item
	Importe     decimal.Decimal `json:"importe,omitempty"`     // Importe total del item

	// Legacy fields
	Name     string          `json:"name"`               // Item name/description (same as Descripcion)
	Amount   decimal.Decimal `json:"amount"`             // Item price (same as Importe)
	IsTaxed  bool            `json:"isTaxed"`            // Whether tax applies to this item
	Quantity int             `json:"quantity,omitempty"` // Quantity (if detected)
}

// ProcessRequest represents the input for invoice processing
type ProcessRequest struct {
	// Image data (base64 encoded or raw bytes will be sent as multipart)
	ImageData []byte `json:"-"`

	// Configuration (optional)
	UseVisionModel bool   `json:"useVisionModel"` // Use vision AI directly (skip OCR)
	AIProvider     string `json:"aiProvider"`     // "openai", "gemini", "ollama"
	Model          string `json:"model"`          // Specific model name
	Language       string `json:"language"`       // OCR language (default: "eng")
}

// ProcessResponse represents the output of invoice processing
type ProcessResponse struct {
	Success bool     `json:"success"`
	Invoice *Invoice `json:"invoice,omitempty"`
	Error   string   `json:"error,omitempty"`

	// Processing metadata
	OCRDuration   float64 `json:"ocrDuration,omitempty"`   // OCR time in seconds
	AIDuration    float64 `json:"aiDuration,omitempty"`    // AI extraction time in seconds
	TotalDuration float64 `json:"totalDuration"`           // Total processing time
}

// Config represents the service configuration
type Config struct {
	// Server config
	Port int    `yaml:"port"`
	Host string `yaml:"host"`

	// OCR config
	OCR OCRConfig `yaml:"ocr"`

	// AI config
	AI AIConfig `yaml:"ai"`

	// Categories (for better extraction)
	Categories []string `yaml:"categories"`
}

// OCRConfig represents OCR-specific configuration
type OCRConfig struct {
	Engine   string `yaml:"engine"`   // "tesseract" or "easyocr"
	Language string `yaml:"language"` // OCR language (default: "eng")
}

// AIConfig represents AI provider configuration
type AIConfig struct {
	// OpenAI
	OpenAI OpenAIConfig `yaml:"openai"`

	// Gemini
	Gemini GeminiConfig `yaml:"gemini"`

	// Ollama (local)
	Ollama OllamaConfig `yaml:"ollama"`

	// Default provider
	DefaultProvider string `yaml:"default_provider"` // "openai", "gemini", "ollama"
}

// OpenAIConfig for OpenAI/Azure OpenAI
type OpenAIConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url,omitempty"` // For custom endpoints
	Model   string `yaml:"model"`              // Default: "gpt-4"
}

// GeminiConfig for Google Gemini
type GeminiConfig struct {
	APIKey string `yaml:"api_key"`
	Model  string `yaml:"model"` // Default: "gemini-pro"
}

// OllamaConfig for local Ollama
type OllamaConfig struct {
	BaseURL string `yaml:"base_url"` // Default: "http://localhost:11434"
	Model   string `yaml:"model"`    // e.g., "mistral", "llama2"
}
