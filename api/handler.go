package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/shopspring/decimal"

	"github.com/facturaIA/invoice-ocr-service/internal/ai"
	"github.com/facturaIA/invoice-ocr-service/internal/auth"
	"github.com/facturaIA/invoice-ocr-service/internal/db"
	"github.com/facturaIA/invoice-ocr-service/internal/models"
	"github.com/facturaIA/invoice-ocr-service/internal/ocr"
	"github.com/facturaIA/invoice-ocr-service/internal/services"
	"github.com/facturaIA/invoice-ocr-service/internal/storage"
)

const (
	MaxUploadSize = 10 * 1024 * 1024 // 10MB
	Version       = "2.1.0"
)

// Handler handles HTTP requests for invoice processing
type Handler struct {
	config *models.Config
}

// NewHandler creates a new API handler
func NewHandler(config *models.Config) *Handler {
	return &Handler{
		config: config,
	}
}

// SetupRoutes configures the HTTP routes
func (h *Handler) SetupRoutes() *mux.Router {
	router := mux.NewRouter()

	// Main endpoints
	router.HandleFunc("/api/process-invoice", h.ProcessInvoice).Methods("POST")
	router.HandleFunc("/api/invoices", h.GetInvoices).Methods("GET")

	// Invoice CRUD
	router.HandleFunc("/api/invoice/{id}", h.GetInvoice).Methods("GET")
	router.HandleFunc("/api/invoice/{id}", h.UpdateInvoice).Methods("PUT")
	router.HandleFunc("/api/invoice/{id}", h.DeleteInvoice).Methods("DELETE")

	// Statistics
	router.HandleFunc("/api/stats", h.GetStats).Methods("GET")

	// Health check
	router.HandleFunc("/health", h.Health).Methods("GET")

	// === ALIAS PARA FRONTEND FACTURAIA ===
	router.HandleFunc("/api/facturas/upload/", h.ProcessInvoice).Methods("POST")
	router.HandleFunc("/api/facturas/mis-facturas/", h.GetClientInvoices).Methods("GET")
	router.HandleFunc("/api/facturas/resumen", h.GetClientStats).Methods("GET")
	router.HandleFunc("/api/facturas/{id}/reprocesar", h.ReprocesarClientInvoice).Methods("POST")
	router.HandleFunc("/api/facturas/{id}/imagen", h.GetClientInvoiceImage).Methods("GET")
	router.HandleFunc("/api/facturas/{id}", h.GetClientInvoice).Methods("GET")
	router.HandleFunc("/api/facturas/{id}", h.DeleteClientInvoice).Methods("DELETE")

	// === VALIDACION IMPUESTOS DGII ===
	router.HandleFunc("/api/v1/invoices/validate", h.ValidateInvoiceTaxes).Methods("POST")

	return router
}

// HealthResponse represents the health check response structure
type HealthResponse struct {
	Status      string            `json:"status"`
	Version     string            `json:"version"`
	Timestamp   string            `json:"timestamp"`
	Uptime      string            `json:"uptime"`
	Memory      MemoryStats       `json:"memory"`
	Tesseract   ServiceStatus     `json:"tesseract"`
	ImageMagick ServiceStatus     `json:"imageMagick"`
	Database    ServiceStatus     `json:"database"`
	Storage     ServiceStatus     `json:"storage"`
	AI          map[string]string `json:"ai"`
}

// MemoryStats represents memory usage statistics
type MemoryStats struct {
	Allocated string `json:"allocated"`
	Total     string `json:"total"`
	System    string `json:"system"`
}

// ServiceStatus represents the status of a service dependency
type ServiceStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

var startTime = time.Now()

// Health endpoint - enhanced for monitoring
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Memory statistics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Check services
	tesseractStatus := h.checkTesseract()
	imageMagickStatus := h.checkImageMagick()
	databaseStatus := h.checkDatabase()
	storageStatus := h.checkStorage()

	// Build response
	response := HealthResponse{
		Status:    "healthy",
		Version:   Version,
		Timestamp: time.Now().Format(time.RFC3339),
		Uptime:    time.Since(startTime).String(),
		Memory: MemoryStats{
			Allocated: fmt.Sprintf("%.2f MB", float64(m.Alloc)/1024/1024),
			Total:     fmt.Sprintf("%.2f MB", float64(m.TotalAlloc)/1024/1024),
			System:    fmt.Sprintf("%.2f MB", float64(m.Sys)/1024/1024),
		},
		Tesseract:   tesseractStatus,
		ImageMagick: imageMagickStatus,
		Database:    databaseStatus,
		Storage:     storageStatus,
		AI: map[string]string{
			"defaultProvider": h.config.AI.DefaultProvider,
			"ocrEngine":       h.config.OCR.Engine,
		},
	}

	// If critical dependencies are down, mark as degraded
	if !tesseractStatus.Available || !imageMagickStatus.Available {
		response.Status = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(response)
}

// checkTesseract verifies Tesseract OCR is available
func (h *Handler) checkTesseract() ServiceStatus {
	cmd := exec.Command("tesseract", "--version")
	output, err := cmd.CombinedOutput()

	if err != nil {
		return ServiceStatus{
			Available: false,
			Error:     "tesseract not found or not executable",
		}
	}

	version := "unknown"
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		version = strings.TrimSpace(lines[0])
	}

	return ServiceStatus{
		Available: true,
		Version:   version,
	}
}

// checkImageMagick verifies ImageMagick is available
func (h *Handler) checkImageMagick() ServiceStatus {
	cmd := exec.Command("convert", "-version")
	output, err := cmd.CombinedOutput()

	if err != nil {
		return ServiceStatus{
			Available: false,
			Error:     "imagemagick not found or not executable",
		}
	}

	version := "unknown"
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		version = strings.TrimSpace(lines[0])
	}

	return ServiceStatus{
		Available: true,
		Version:   version,
	}
}

// checkDatabase verifies PostgreSQL connection
func (h *Handler) checkDatabase() ServiceStatus {
	if db.Pool == nil {
		return ServiceStatus{
			Available: false,
			Error:     "database pool not initialized",
		}
	}

	return ServiceStatus{
		Available: true,
		Version:   "PostgreSQL via PgBouncer",
	}
}

// checkStorage verifies MinIO connection
func (h *Handler) checkStorage() ServiceStatus {
	if storage.Client == nil {
		return ServiceStatus{
			Available: false,
			Error:     "storage client not initialized",
		}
	}

	return ServiceStatus{
		Available: true,
		Version:   "MinIO S3",
	}
}

// ProcessInvoice handles invoice processing with multi-tenant support
func (h *Handler) ProcessInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	startTime := time.Now()

	// Get claims from JWT
	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized: "+err.Error())
		return
	}

	// Parse multipart form
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)
	err = r.ParseMultipartForm(MaxUploadSize)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, "File too large or invalid form data")
		return
	}

	// Get file - accept both "file" and "image" field names
	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("image")
		if err != nil {
			h.sendError(w, http.StatusBadRequest, "No file provided (use 'file' or 'image' field)")
			return
		}
	}
	defer file.Close()

	// Read file bytes
	imageData, err := io.ReadAll(file)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to read file")
		return
	}

	// Get optional parameters
	aiProvider := r.FormValue("aiProvider")
	if aiProvider == "" {
		aiProvider = h.config.AI.DefaultProvider
	}

	// Default to vision mode for Gemini and OpenAI (Claude via proxy supports vision)
	useVisionModelParam := r.FormValue("useVisionModel")
	useVisionModel := useVisionModelParam == "true" || (useVisionModelParam == "" && (aiProvider == "gemini" || aiProvider == "openai"))

	model := r.FormValue("model")
	language := r.FormValue("language")
	if language == "" {
		language = h.config.OCR.Language
	}

	// Generate unique filename
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	filename := fmt.Sprintf("%s_%s%s",
		time.Now().Format("20060102_150405"),
		uuid.New().String()[:8],
		storage.GetFileExtension(contentType),
	)

	// Upload to MinIO (if configured)
	var imagenURL string
	if storage.Client != nil {
		imageReader := bytes.NewReader(imageData)
		imagenURL, err = storage.UploadInvoiceImage(
			ctx,
			claims.EmpresaAlias,
			filename,
			imageReader,
			int64(len(imageData)),
			contentType,
		)
		if err != nil {
			// Log but don't fail - image storage is optional
			fmt.Printf("Warning: failed to upload image to MinIO: %v\n", err)
		}
	}

	// Process OCR
	invoice, ocrDuration, aiDuration, _, err := h.processInvoice(
		imageData,
		useVisionModel,
		aiProvider,
		model,
		language,
	)

	totalDuration := time.Since(startTime).Seconds()

	if err != nil {
		response := models.ProcessResponse{
			Success:       false,
			Error:         err.Error(),
			TotalDuration: totalDuration,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// === PASO: Validación cruzada de impuestos ===
	validationInput := &services.InvoiceInput{
		MontoServicios:        decimalToFloat64(invoice.Subtotal),
		MontoBienes:           0,
		Descuento:             decimalToFloat64(invoice.Descuento),
		ITBISFacturado:        decimalToFloat64(invoice.ITBIS),
		ITBISTasa:             decimalToFloat64(invoice.ITBISTasa),
		ITBISExento:           decimalToFloat64(invoice.ITBISExento),
		ITBISRetenido:         decimalToFloat64(invoice.ITBISRetenido),
		ITBISProporcionalidad: decimalToFloat64(invoice.ITBISProporcionalidad),
		ITBISCosto:            decimalToFloat64(invoice.ITBISCosto),
		ISCMonto:              decimalToFloat64(invoice.ISC),
		ISCCategoria:          invoice.ISCCategoria,
		CDTMonto:              decimalToFloat64(invoice.CDTMonto),
		Cargo911:              decimalToFloat64(invoice.Cargo911),
		PropinaLegal:          decimalToFloat64(invoice.Propina),
		OtrosImpuestos:        decimalToFloat64(invoice.OtrosImpuestos),
		MontoNoFacturable:     decimalToFloat64(invoice.MontoNoFacturable),
		RetencionISRTipo:      invoice.RetencionISRTipo,
		RetencionISRMonto:     decimalToFloat64(invoice.ISR),
		TotalFactura:          decimalToFloat64(invoice.Total),
		NCF:                   invoice.NCF,
	}

	validator := services.NewTaxValidator()
	validationResult := validator.Validate(validationInput)

	// Determinar extraction_status según validación y confidence
	extractionStatus := "validated"
	if !validationResult.Valid {
		extractionStatus = "error"
	} else if validationResult.NeedsReview || invoice.Confidence < 0.85 {
		extractionStatus = "review"
		validationResult.NeedsReview = true
	}

	// Serializar errores/warnings para review_notes
	reviewNotes := ""
	if len(validationResult.Errors) > 0 || len(validationResult.Warnings) > 0 {
		if rn, err := json.Marshal(validationResult); err == nil {
			reviewNotes = string(rn)
		}
	}

	// Save to facturas_clientes (client mobile app table)
	var savedClientInvoice *db.ClientInvoice
	if db.Pool != nil && invoice != nil {
		// Parse fecha
		var fechaDoc *time.Time
		if !invoice.FechaFactura.IsZero() {
			t := invoice.FechaFactura
			fechaDoc = &t
		} else if !invoice.Date.IsZero() {
			t := invoice.Date
			fechaDoc = &t
		}

		// Build OCR notes summary
		ocrNotes := ""
		if ocrJSON, err := json.Marshal(invoice); err == nil {
			ocrNotes = string(ocrJSON)
		}

		// Serialize items to JSON
		itemsJSON := ""
		if len(invoice.Items) > 0 {
			if ij, err := json.Marshal(invoice.Items); err == nil {
				itemsJSON = string(ij)
			}
		}

		// Determinar estado según extraction_status
		estado := "pendiente"
		if extractionStatus == "validated" {
			estado = "procesado"
		}

		clientInvoice := &db.ClientInvoice{
			ClienteID:        claims.UserID,
			ArchivoURL:       imagenURL,
			ArchivoNombre:    "factura_scan.jpg",
			TipoDocumento:    invoice.TipoNCF,
			FechaDocumento:   fechaDoc,
			Monto:            decimalToFloat64(invoice.Total),
			NCF:              invoice.NCF,
			Proveedor:        invoice.NombreEmisor,
			Estado:           estado,
			NotasCliente:     "",
			EmisorRNC:        invoice.RNCEmisor,
			ReceptorNombre:   invoice.NombreReceptor,
			ReceptorRNC:      invoice.RNCReceptor,
			// Montos base
			Subtotal:  decimalToFloat64(invoice.Subtotal),
			Descuento: decimalToFloat64(invoice.Descuento),
			// ITBIS
			ITBIS:                 decimalToFloat64(invoice.ITBIS),
			ITBISRetenido:         decimalToFloat64(invoice.ITBISRetenido),
			ITBISExento:           decimalToFloat64(invoice.ITBISExento),
			ITBISProporcionalidad: decimalToFloat64(invoice.ITBISProporcionalidad),
			ITBISCosto:            decimalToFloat64(invoice.ITBISCosto),
			// ISR
			ISR:              decimalToFloat64(invoice.ISR),
			RetencionISRTipo: intToPtr(invoice.RetencionISRTipo),
			// ISC
			ISC:          decimalToFloat64(invoice.ISC),
			ISCCategoria: invoice.ISCCategoria,
			// Otros cargos
			CDTMonto:          decimalToFloat64(invoice.CDTMonto),
			Cargo911:          decimalToFloat64(invoice.Cargo911),
			Propina:           decimalToFloat64(invoice.Propina),
			OtrosImpuestos:    decimalToFloat64(invoice.OtrosImpuestos),
			MontoNoFacturable: decimalToFloat64(invoice.MontoNoFacturable),
			// Clasificación
			FormaPago:        invoice.FormaPago,
			TipoNCF:          invoice.TipoNCF,
			TipoBienServicio: invoice.TipoBienServicio,
			ConfidenceScore:  invoice.Confidence,
			RawOCRJSON:       ocrNotes,
			ItemsJSON:        itemsJSON,
			ExtractionStatus: extractionStatus,
			ReviewNotes:      reviewNotes,
		}

		if err := db.SaveClientInvoice(ctx, clientInvoice); err != nil {
			fmt.Printf("Warning: failed to save client invoice to DB: %v\n", err)
		} else {
			savedClientInvoice = clientInvoice
		}
	}

	// Build data object with campos fiscales DGII completos
	dataMap := map[string]interface{}{
		// Identificación
		"ncf":              invoice.NCF,
		"tipo_ncf":         invoice.TipoNCF,
		"emisor_rnc":       invoice.RNCEmisor,
		"emisor_nombre":    invoice.NombreEmisor,
		"fecha_documento":  invoice.FechaFactura,

		// Montos base
		"monto_servicios":  decimalToFloat64(invoice.Subtotal),
		"monto_bienes":     0,
		"descuento":        decimalToFloat64(invoice.Descuento),

		// ITBIS
		"itbis_facturado":        decimalToFloat64(invoice.ITBIS),
		"itbis_tasa":             decimalToFloat64(invoice.ITBISTasa),
		"itbis_exento":           decimalToFloat64(invoice.ITBISExento),
		"itbis_proporcionalidad": decimalToFloat64(invoice.ITBISProporcionalidad),
		"itbis_costo":            decimalToFloat64(invoice.ITBISCosto),
		"itbis_retenido":         decimalToFloat64(invoice.ITBISRetenido),

		// ISC
		"isc_monto":     decimalToFloat64(invoice.ISC),
		"isc_categoria": invoice.ISCCategoria,

		// Otros cargos
		"cdt_monto":           decimalToFloat64(invoice.CDTMonto),
		"cargo_911":           decimalToFloat64(invoice.Cargo911),
		"propina_legal":       decimalToFloat64(invoice.Propina),
		"otros_impuestos":     decimalToFloat64(invoice.OtrosImpuestos),
		"monto_no_facturable": decimalToFloat64(invoice.MontoNoFacturable),

		// Retenciones ISR
		"retencion_isr_tipo":  invoice.RetencionISRTipo,
		"retencion_isr_monto": decimalToFloat64(invoice.ISR),

		// Total
		"total_factura":    decimalToFloat64(invoice.Total),

		// Metadata
		"confidence_score": invoice.Confidence,
		"forma_pago":       invoice.FormaPago,
		"tipo_bien_servicio": invoice.TipoBienServicio,
		"imagen_url":       imagenURL,
		"items":            invoice.Items,
	}

	// Build response con nuevo formato
	responseData := map[string]interface{}{
		"success":           true,
		"extraction_status": extractionStatus,
		"data":              dataMap,
		"validation":        validationResult,
		"ocrDuration":       ocrDuration,
		"aiDuration":        aiDuration,
		"totalDuration":     totalDuration,
	}

	// Add saved invoice info if available
	if savedClientInvoice != nil {
		responseData["invoice_id"] = savedClientInvoice.ID
		responseData["created_at"] = savedClientInvoice.CreatedAt
		// Use proxy URL so mobile app can access the image
		dataMap["imagen_url"] = fmt.Sprintf("/api/facturas/%s/imagen", savedClientInvoice.ID)
		responseData["saved_to_db"] = true
	} else {
		responseData["saved_to_db"] = false
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(responseData)
}

// GetInvoices returns invoices for the authenticated user's empresa
func (h *Handler) GetInvoices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()

	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	invoices, err := db.GetInvoices(ctx, claims.EmpresaAlias, 100)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get invoices: %v", err))
		return
	}

	// Generate presigned URLs for images
	for i := range invoices {
		if invoices[i].ImagenURL != "" && storage.Client != nil {
			if presignedURL, err := storage.GetPresignedURL(ctx, invoices[i].ImagenURL); err == nil {
				invoices[i].ImagenURL = presignedURL
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"invoices":      invoices,
		"count":         len(invoices),
		"empresa_alias": claims.EmpresaAlias,
	})
}

// GetInvoice returns a single invoice
func (h *Handler) GetInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	vars := mux.Vars(r)
	invoiceID := vars["id"]

	invoice, err := db.GetInvoiceByID(ctx, claims.EmpresaAlias, invoiceID)
	if err != nil {
		fmt.Printf("GetInvoiceByID error: %v (empresa=%s, id=%s)\n", err, claims.EmpresaAlias, invoiceID)
		h.sendError(w, http.StatusNotFound, fmt.Sprintf("invoice not found: %v", err))
		return
	}

	// Generate presigned URL for image
	if invoice.ImagenURL != "" && storage.Client != nil {
		if presignedURL, err := storage.GetPresignedURL(ctx, invoice.ImagenURL); err == nil {
			invoice.ImagenURL = presignedURL
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"invoice":       invoice,
		"empresa_alias": claims.EmpresaAlias,
	})
}

// UpdateInvoice updates invoice data
func (h *Handler) UpdateInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	vars := mux.Vars(r)
	invoiceID := vars["id"]

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Only allow certain fields to be updated
	allowed := map[string]bool{
		"estado":           true,
		"ncf":              true,
		"tipo_gasto":       true,
		"rnc_proveedor":    true,
		"nombre_proveedor": true,
		"subtotal":         true,
		"itbis":            true,
		"total":            true,
	}
	filtered := make(map[string]interface{})
	for k, v := range updates {
		if allowed[k] {
			filtered[k] = v
		}
	}

	if len(filtered) == 0 {
		h.sendError(w, http.StatusBadRequest, "no valid fields to update")
		return
	}

	if err := db.UpdateInvoice(ctx, claims.EmpresaAlias, invoiceID, filtered); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to update invoice")
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "invoice updated",
	})
}

// DeleteInvoice removes an invoice
func (h *Handler) DeleteInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	vars := mux.Vars(r)
	invoiceID := vars["id"]

	// Optionally: delete image from MinIO
	if storage.Client != nil {
		invoice, err := db.GetInvoiceByID(ctx, claims.EmpresaAlias, invoiceID)
		if err == nil && invoice.ImagenURL != "" {
			// Delete image (ignore errors)
			_ = storage.DeleteImage(ctx, invoice.ImagenURL)
		}
	}

	if err := db.DeleteInvoice(ctx, claims.EmpresaAlias, invoiceID); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to delete invoice")
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "invoice deleted",
	})
}

// GetStats returns monthly statistics
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	stats, err := db.GetMonthlyStats(ctx, claims.EmpresaAlias)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"stats":         stats,
		"empresa_alias": claims.EmpresaAlias,
	})
}

// processInvoice performs the actual processing and returns OCR text
func (h *Handler) processInvoice(
	imageData []byte,
	useVisionModel bool,
	providerName string,
	modelName string,
	language string,
) (*models.Invoice, float64, float64, string, error) {
	var ocrText string
	var ocrDuration float64
	var imageBase64 string

	// Step 2: OCR or prepare image for vision model
	if useVisionModel {
		// For AI vision models (Gemini), send the ORIGINAL image - no grayscale
		// Gemini reads color images better than grayscale preprocessed ones
		imageBase64 = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(imageData)
		fmt.Printf("[Process] Using original image for vision model (%d bytes)\n", len(imageData))
	} else {
		// For Tesseract OCR, preprocess with grayscale+contrast
		preprocessor := ocr.NewPreprocessor(h.config.OCR.Engine == "easyocr")
		processedImage, err := preprocessor.PreprocessImageFromBytes(imageData)
		if err != nil {
			return nil, 0, 0, "", fmt.Errorf("image preprocessing failed: %w", err)
		}
		tesseract := ocr.NewTesseractOCR(language)
		text, duration, err := tesseract.ExtractText(processedImage)
		if err != nil {
			return nil, 0, 0, "", fmt.Errorf("OCR failed: %w", err)
		}
		ocrText = text
		ocrDuration = duration
	}

	// Step 3: Create AI provider
	provider, err := h.createProvider(providerName, modelName)
	if err != nil {
		return nil, ocrDuration, 0, ocrText, err
	}

	// Step 4: Extract data with AI
	extractor := ai.NewExtractor(provider, h.config.Categories)
	invoice, aiDuration, err := extractor.Extract(ocrText, imageBase64)
	if err != nil {
		return nil, ocrDuration, 0, ocrText, fmt.Errorf("AI extraction failed: %w", err)
	}

	// Store raw text in invoice
	// invoice.RawText = ocrText // Comentado - extractor ya lo maneja con campos DGII

	return invoice, ocrDuration, aiDuration, ocrText, nil
}

// createProvider creates the appropriate AI provider
func (h *Handler) createProvider(providerName, modelName string) (ai.Provider, error) {
	switch providerName {
	case "openai":
		model := modelName
		if model == "" {
			model = h.config.AI.OpenAI.Model
		}
		return ai.NewOpenAIProvider(
			h.config.AI.OpenAI.APIKey,
			h.config.AI.OpenAI.BaseURL,
			model,
		), nil

	case "gemini":
		model := modelName
		if model == "" {
			model = h.config.AI.Gemini.Model
		}
		return ai.NewGeminiProvider(
			h.config.AI.Gemini.APIKey,
			model,
		), nil

	case "ollama":
		model := modelName
		if model == "" {
			model = h.config.AI.Ollama.Model
		}
		return ai.NewOllamaProvider(
			h.config.AI.Ollama.BaseURL,
			model,
		), nil

	default:
		return nil, fmt.Errorf("unsupported AI provider: %s", providerName)
	}
}

// sendError sends an error response
func (h *Handler) sendError(w http.ResponseWriter, statusCode int, message string) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// decimalToFloat64 converts decimal.Decimal to float64
func decimalToFloat64(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

func intToPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

// ValidateInvoiceTaxes validates tax fields from OCR/AI extraction
// POST /api/v1/invoices/validate
func (h *Handler) ValidateInvoiceTaxes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse input
	var input services.InvoiceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Validate
	validator := services.NewTaxValidator()
	result := validator.Validate(&input)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}
