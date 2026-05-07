package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	router.Handle("/api/facturas/{id}/reprocesar", auth.RequireRole("admin", "contador")(http.HandlerFunc(h.ReprocesarClientInvoice))).Methods("POST")
	router.HandleFunc("/api/facturas/{id}/imagen", h.GetClientInvoiceImage).Methods("GET")
	router.HandleFunc("/api/facturas/{id}", h.GetClientInvoice).Methods("GET")
	router.HandleFunc("/api/facturas/{id}", h.DeleteClientInvoice).Methods("DELETE")
	// [WB1] BUG-01/BUG-03 fix — review/approve flow: escribir public.facturas_clientes
	// para disparar triggers BD (auto_tag_factura_606 + auto_set_receptor_rnc).
	router.HandleFunc("/api/facturas/{id}/update", h.UpdateClientInvoiceHandler).Methods("PUT")
	router.HandleFunc("/api/facturas/{id}/approve", h.ApproveClientInvoiceHandler).Methods("PUT")

	// === VALIDACION IMPUESTOS DGII ===
	router.HandleFunc("/api/v1/invoices/validate", h.ValidateInvoiceTaxes).Methods("POST")

	// Error reporting endpoint (no auth required - receives errors from mobile app)
	router.HandleFunc("/api/errors", h.ReceiveErrorReport).Methods("POST")

	// === SHAREPOINT SYNC MONITORING ===
	router.Handle("/api/admin/sharepoint-queue", auth.RequireRole("admin")(http.HandlerFunc(h.GetSharePointQueueStatus))).Methods("GET")

	// === FORMATO 606 DGII ===
	router.HandleFunc("/api/formato-606/{rnc_receptor}/preview", h.GetFormato606Preview).Methods("GET")
	router.HandleFunc("/api/formato-606/{rnc_receptor}/validate", h.ValidateFormato606).Methods("POST")
	router.HandleFunc("/api/formato-606/{rnc_receptor}", h.GetFormato606).Methods("GET")
	router.HandleFunc("/api/formato-606/factura/{id}/toggle-aplica606", h.ToggleAplica606).Methods("PUT")
	router.HandleFunc("/api/envios-606/{id}/referencia", h.UpdateEnvio606Referencia).Methods("PUT")

	// === FORMATO 608 DGII (NCF Anulados) ===
	router.HandleFunc("/api/formato-608/{rnc}", h.GetFormato608).Methods("GET")

	// === P1 ANTI-DUPLICADAS NCF — pre-save check ===
	router.HandleFunc("/api/facturas/check-duplicate", h.CheckDuplicateNCF).Methods("GET")

	return router
}

// CheckDuplicateNCF handles GET /api/facturas/check-duplicate?ncf=X&tipo=606|607
//
// Multi-tenant safety: empresa_id is derived inside the DB function from the
// clientes table via claims.UserID (cliente_id from JWT). Never trusted from
// query params.
//
// Response 200:
//
//	{"exists": false}
//	{"exists": true, "existing": {"id": "...", "ncf": "...", "fecha_documento": "...", "monto": 0.0}}
func (h *Handler) CheckDuplicateNCF(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	ncf := r.URL.Query().Get("ncf")
	tipo := r.URL.Query().Get("tipo") // "606" o "607"
	if tipo != "606" && tipo != "607" {
		tipo = "606" // default
	}

	// NCF vacío → siempre permitir (UNIQUE PARCIAL migration 20260501)
	if ncf == "" {
		json.NewEncoder(w).Encode(map[string]any{"exists": false})
		return
	}

	result, err := db.CheckDuplicateNCFByTipo(ctx, claims.UserID, ncf, tipo)
	if err != nil {
		// DB error → fail-open: allow save + log warning (no bloquear UX)
		log.Printf("[CheckDuplicateNCF] DB error cliente=%s ncf=%s: %v", claims.UserID, ncf, err)
		json.NewEncoder(w).Encode(map[string]any{"exists": false})
		return
	}

	if !result.Exists {
		json.NewEncoder(w).Encode(map[string]any{"exists": false})
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"exists": true,
		"existing": map[string]any{
			"id":              result.ID,
			"ncf":             result.NCF,
			"fecha_documento": result.Fecha,
			"monto":           result.Monto,
		},
	})
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
		sendAppError(w, ErrFileTooLarge)
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
		if errors.Is(err, ai.ErrAllProvidersFailed) {
			// All AI providers failed transiently — save invoice for manual review
			// The image is already in MinIO (uploaded before processInvoice was called)
			log.Printf("[OCR] All AI providers failed: %v. Saving as revision_manual.", err)
			if db.Pool != nil && imagenURL != "" {
				manualInvoice := &db.ClientInvoice{
					ClienteID:        claims.UserID,
					ArchivoURL:       imagenURL,
					ArchivoNombre:    "factura_scan.jpg",
					Estado:           "procesado",
					ExtractionStatus: "revision_manual",
					ReviewNotes:      fmt.Sprintf(`{"error":"all_providers_failed","detail":%q}`, err.Error()),
				}
				if saveErr := db.SaveClientInvoice(ctx, manualInvoice); saveErr != nil {
					log.Printf("[OCR] Failed to save revision_manual invoice: %v", saveErr)
				}
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":           true,
				"extraction_status": "revision_manual",
				"user_message":      "Tu factura fue guardada. La IA no está disponible temporalmente. Un contador la revisará pronto.",
				"totalDuration":     time.Since(startTime).Seconds(),
			})
			return
		}
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

	// Calcular montoServicios/montoBienes con fallback al subtotal
	montoServicios := decimalToFloat64(invoice.MontoServicios)
	montoBienes := decimalToFloat64(invoice.MontoBienes)
	// Fallback: si la IA no separó servicios/bienes, usar subtotal
	if montoServicios == 0 && montoBienes == 0 {
		montoServicios = decimalToFloat64(invoice.Subtotal)
	}

	// FechaPago: solo si no es zero
	fechaPagoStr := ""
	if !invoice.FechaPago.IsZero() {
		fechaPagoStr = invoice.FechaPago.Format("2006-01-02")
	}

	// NCFVencimiento: solo si no es zero
	ncfVencimientoStr := ""
	if !invoice.FechaVencimiento.IsZero() {
		ncfVencimientoStr = invoice.FechaVencimiento.Format("2006-01-02")
	}

	validationInput := &services.InvoiceInput{
		MontoServicios:          montoServicios,
		MontoBienes:             montoBienes,
		Descuento:               decimalToFloat64(invoice.Descuento),
		ITBISFacturado:          decimalToFloat64(invoice.ITBIS),
		ITBISTasa:               decimalToFloat64(invoice.ITBISTasa),
		ITBISExento:             decimalToFloat64(invoice.ITBISExento),
		ITBISRetenido:           decimalToFloat64(invoice.ITBISRetenido),
		ITBISProporcionalidad:   decimalToFloat64(invoice.ITBISProporcionalidad),
		ITBISCosto:              decimalToFloat64(invoice.ITBISCosto),
		ISCMonto:                decimalToFloat64(invoice.ISC),
		ISCCategoria:            invoice.ISCCategoria,
		CDTMonto:                decimalToFloat64(invoice.CDTMonto),
		Cargo911:                decimalToFloat64(invoice.Cargo911),
		PropinaLegal:            decimalToFloat64(invoice.Propina),
		OtrosImpuestos:          decimalToFloat64(invoice.OtrosImpuestos),
		MontoNoFacturable:       decimalToFloat64(invoice.MontoNoFacturable),
		RetencionISRTipo:        invoice.RetencionISRTipo,
		RetencionISRMonto:       decimalToFloat64(invoice.ISR),
		TotalFactura:            decimalToFloat64(invoice.Total),
		NCF:                     invoice.NCF,
		NCFModifica:             invoice.NCFModifica,
		TipoNCF:                 invoice.TipoNCF,
		ITBISRetenidoPorcentaje: invoice.ITBISRetenidoPorcentaje,
		FechaPago:               fechaPagoStr,
		NCFVencimiento:          ncfVencimientoStr,
		// Emisor identity — for RNC length validation (W17.5)
		NombreEmisor: invoice.NombreEmisor,
		RNCEmisor:    invoice.RNCEmisor,
		// W17.1 — IA fiscal skill fields: let IA override strict ITBIS validation
		CategoriaITBIS:     invoice.CategoriaITBIS,
		SectorProveedor:    invoice.SectorProveedor,
		RequiereCorreccion: invoice.RequiereCorreccion,
		WarningsIA:         invoice.WarningsIA,
	}

	validator := services.NewTaxValidator()
	validationResult := validator.Validate(validationInput)

	// Determinar extraction_status según validación y confidence
	extractionStatus := "validated"
	if !validationResult.Valid {
		extractionStatus = "error"
	} else if validationResult.NeedsReview && invoice.Confidence < 0.75 {
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
	rncMismatchWarning := ""
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

		// Toda factura escaneada exitosamente = "procesado" para el usuario
		// extraction_status y review_notes guardan los detalles internos para el contador
		estado := "procesado"

		// === VALIDACION 1: Duplicados ===
		if invoice.NCF != "" {
			// Con NCF: dedup exacto por NCF + emisor
			isDup, dupErr := db.CheckDuplicateNCF(ctx, claims.UserID, invoice.NCF, invoice.RNCEmisor)
			if dupErr == nil && isDup {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":      false,
					"error_code":   "DUPLICATE_NCF",
					"error":        fmt.Sprintf("Ya existe una factura con NCF %s del mismo proveedor", invoice.NCF),
					"user_message": fmt.Sprintf("Ya tienes registrada una factura con NCF %s de este proveedor (RNC %s). No se guardó para evitar duplicados.", invoice.NCF, invoice.RNCEmisor),
				})
				return
			}
		} else {
			// Sin NCF: dedup por monto + emisor + fecha + hora
			total := decimalToFloat64(invoice.Total)
			isDup, dupErr := db.CheckDuplicateByAmount(ctx, claims.UserID, total, invoice.RNCEmisor, fechaDoc, invoice.HoraFactura)
			if dupErr == nil && isDup {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":      false,
					"error_code":   "DUPLICATE_AMOUNT",
					"error":        "Factura duplicada detectada",
					"user_message": "Ya existe una factura del mismo proveedor, mismo monto, misma fecha y hora. Si es una factura diferente, verifique que la hora sea distinta.",
				})
				return
			}
		}

		// === CLASIFICACION: TipoFactura post-OCR ===
		// Override tipoFactura según si el emisor coincide con el RNC de la empresa del usuario.
		// Bug fix: factura 51e05b7f tenía tipoFactura='gastos' cuando emisor_rnc=131047939 (HUYGHU=empresa).
		// Lógica: emisor_rnc == empresa_rnc → "ventas" (607); emisor_rnc != empresa_rnc → "gastos" (606).
		clientRNC, _ := db.GetClientRNC(ctx, claims.UserID)
		ai.ClassifyTipoFactura(invoice, clientRNC)

		// === VALIDACION 2: Receptor RNC — advertencia DGII ===
		// Se guarda la factura siempre, pero se advierte si el RNC no sirve para devengar impuestos
		if clientRNC != "" {
			if invoice.RNCReceptor == "" {
				// Factura sin RNC receptor
				rncMismatchWarning = fmt.Sprintf("Esta factura no tiene RNC receptor. Sin este dato (su RNC: %s) no podrá devengar impuestos en la DGII.", clientRNC)
			} else {
				normalizedReceptorRNC := strings.ReplaceAll(invoice.RNCReceptor, "-", "")
				if clientRNC != normalizedReceptorRNC {
					// RNC receptor no coincide con el usuario logueado
					rncMismatchWarning = fmt.Sprintf("El RNC receptor de esta factura (%s) no coincide con su RNC (%s). Sin el RNC correcto no podrá devengar impuestos en la DGII.", invoice.RNCReceptor, clientRNC)
				}
			}
		}

		// FechaPago para BD
		var fechaPago *time.Time
		if !invoice.FechaPago.IsZero() {
			t := invoice.FechaPago
			fechaPago = &t
		}

		clientInvoice := &db.ClientInvoice{
			ClienteID:        claims.UserID,
			ArchivoURL:       imagenURL,
			ArchivoNombre:    "factura_scan.jpg",
			TipoDocumento:    invoice.TipoNCF,
			HoraFactura:      invoice.HoraFactura,
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
			// Campos nuevos
			ITBISTasa:               decimalToFloat64(invoice.ITBISTasa),
			NCFModifica:             invoice.NCFModifica,
			TipoIDEmisor:            invoice.TipoIDEmisor,
			TipoIDReceptor:          invoice.TipoIDReceptor,
			MontoServicios:          decimalToFloat64(invoice.MontoServicios),
			MontoBienes:             decimalToFloat64(invoice.MontoBienes),
			ITBISRetenidoPorcentaje: invoice.ITBISRetenidoPorcentaje,
			FechaPago:               fechaPago,
		}

		if err := db.SaveClientInvoice(ctx, clientInvoice); err != nil {
			log.Printf("ERROR SaveClientInvoice: %v (cliente_id=%s emisor_rnc=%s ncf=%s monto=%v)", err, clientInvoice.ClienteID, clientInvoice.EmisorRNC, clientInvoice.NCF, clientInvoice.Monto)
			http.Error(w, fmt.Sprintf("DB save failed: %v", err), http.StatusInternalServerError)
			return
		} else {
			savedClientInvoice = clientInvoice

			// Queue for SharePoint sync (non-blocking)
			go func(facturaID, clienteID, rncCliente string, fechaFactura *time.Time, archivoURL, archivoNombre string) {
				if archivoURL != "" && db.Pool != nil {
					_, qErr := db.Pool.Exec(context.Background(),
						`INSERT INTO sharepoint_sync_queue (factura_id, cliente_id, rnc_cliente, fecha_factura, archivo_url, archivo_nombre)
						 VALUES ($1, $2, $3, $4, $5, $6)
						 ON CONFLICT DO NOTHING`,
						facturaID, clienteID, rncCliente, fechaFactura, archivoURL, archivoNombre)
					if qErr != nil {
						log.Printf("[SharePoint Queue] Error queueing factura %s: %v", facturaID, qErr)
					} else {
						log.Printf("[SharePoint Queue] Queued factura %s for sync", facturaID)
					}
				}
			}(clientInvoice.ID, clientInvoice.ClienteID, claims.EmpresaAlias, fechaDoc, imagenURL, filename)
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
		"proveedor":        invoice.NombreEmisor,
		"subtotal":         decimalToFloat64(invoice.Subtotal),
		"monto_servicios":  montoServicios,
		"monto_bienes":     montoBienes,
		"descuento":        decimalToFloat64(invoice.Descuento),
		"ncf_modifica":     invoice.NCFModifica,
		"itbis_retenido_porcentaje": invoice.ITBISRetenidoPorcentaje,

		// ITBIS
		"itbis":                  decimalToFloat64(invoice.ITBIS),
		"itbis_tasa":             decimalToFloat64(invoice.ITBISTasa),
		"itbis_exento":           decimalToFloat64(invoice.ITBISExento),
		"itbis_proporcionalidad": decimalToFloat64(invoice.ITBISProporcionalidad),
		"itbis_costo":            decimalToFloat64(invoice.ITBISCosto),
		"itbis_retenido":         decimalToFloat64(invoice.ITBISRetenido),

		// ISC
		"isc":           decimalToFloat64(invoice.ISC),
		"isc_categoria": invoice.ISCCategoria,

		// Otros cargos
		"cdt_monto":           decimalToFloat64(invoice.CDTMonto),
		"cargo_911":           decimalToFloat64(invoice.Cargo911),
		"propina":             decimalToFloat64(invoice.Propina),
		"otros_impuestos":     decimalToFloat64(invoice.OtrosImpuestos),
		"monto_no_facturable": decimalToFloat64(invoice.MontoNoFacturable),

		// Retenciones ISR
		"retencion_isr_tipo": invoice.RetencionISRTipo,
		"isr":                decimalToFloat64(invoice.ISR),

		// Total
		"monto": decimalToFloat64(invoice.Total),
		"total": decimalToFloat64(invoice.Total),

		// Metadata
		"confidence_score": invoice.Confidence,
		"forma_pago":       invoice.FormaPago,
		"tipo_bien_servicio": invoice.TipoBienServicio,
		"imagen_url":       imagenURL,
		"items":            invoice.Items,
	}

	// Determinar user_message según extraction_status
	var userMessage string
	switch extractionStatus {
	case "error":
		userMessage = "No pudimos extraer la información. Intenta con mejor iluminación."
	case "review":
		userMessage = "Algunos datos necesitan revisión. Por favor verifica los campos marcados."
	default:
		userMessage = "Factura procesada exitosamente."
	}

	// Build response con nuevo formato
	responseData := map[string]interface{}{
		"success":           true,
		"warning":           rncMismatchWarning,
		"extraction_status": extractionStatus,
		"user_message":      userMessage,
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
		sendAppError(w, ErrDBUnavailable)
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
		sendAppError(w, ErrDBUnavailable)
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
		sendAppError(w, ErrDBUnavailable)
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
		"aplica_606":       true,
		"aplica_607":       true,
		"periodo_606":      true,
		"periodo_607":      true,
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
		sendAppError(w, ErrDBUnavailable)
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
		sendAppError(w, ErrDBUnavailable)
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
		primary := ai.NewOpenAIProvider(
			h.config.AI.OpenAI.APIKey,
			h.config.AI.OpenAI.BaseURL,
			model,
		)
		// Fallback: openrouter-gemma-27b — free vision model via CLIProxy
		fallback := ai.NewOpenAIProvider(
			h.config.AI.OpenAI.APIKey,
			h.config.AI.OpenAI.BaseURL,
			"openrouter-gemma-27b",
		)
		return ai.NewFallbackProvider(
			ai.NamedProvider(model, primary),
			ai.NamedProvider("openrouter-gemma-27b", fallback),
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

// GetSharePointQueueStatus returns the current status counts of the SharePoint sync queue
// GET /api/admin/sharepoint-queue
func (h *Handler) GetSharePointQueueStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	rows, err := db.Pool.Query(r.Context(), `
		SELECT status, COUNT(*) as count
		FROM sharepoint_sync_queue
		GROUP BY status
		ORDER BY status
	`)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if scanErr := rows.Scan(&status, &count); scanErr == nil {
			result[status] = count
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

// ReceiveErrorReport receives error reports from the mobile app
func (h *Handler) ReceiveErrorReport(w http.ResponseWriter, r *http.Request) {
	var report struct {
		Message   string                 `json:"message"`
		Stack     string                 `json:"stack"`
		Context   map[string]interface{} `json:"context"`
		Timestamp string                 `json:"timestamp"`
		Level     string                 `json:"level"`
	}

	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		sendJSON(w, 200, map[string]string{"status": "ok", "note": "invalid json ignored"})
		return
	}

	// Log to stdout always
	log.Printf("[ERROR_REPORT] level=%s message=%s", report.Level, report.Message)

	// Save to DB if available
	if db.Pool != nil {
		ctx := r.Context()
		contextJSON, _ := json.Marshal(report.Context)
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO error_logs (message, stack, context, level, source) VALUES ($1, $2, $3, $4, 'mobile')`,
			report.Message, report.Stack, contextJSON, report.Level)
		if err != nil {
			log.Printf("[ERROR_REPORT] Failed to save to DB: %v", err)
		}
	}

	sendJSON(w, 200, map[string]string{"status": "received"})
}

// =============================================================================
// [WB1] Hito facturaia-bugs-p0-invoice-review-w2 — KB 8705 audit Wave A
// Fixes BUG-01 (endpoint /update no existía → 404) + BUG-03 (handler legacy
// escribía <schema>.facturas SIN triggers BD).
//
// Estos handlers escriben public.facturas_clientes via db.UpdateClientInvoice
// helper, lo que dispara los triggers BD:
//   - auto_tag_factura_606 (calcula aplica_606, periodo_606, itbis_adelantar)
//   - auto_set_receptor_rnc (rellena receptor_rnc desde clientes.rnc)
// =============================================================================

// applyClientInvoicePayload — mapea payload JSON (nombres app/BD) sobre el
// struct ClientInvoice cargado desde BD. Solo modifica campos del allowlist.
// Devuelve la cantidad de campos efectivamente aplicados.
func applyClientInvoicePayload(inv *db.ClientInvoice, payload map[string]interface{}) int {
	applied := 0

	getStr := func(v interface{}) (string, bool) {
		if s, ok := v.(string); ok {
			return s, true
		}
		return "", false
	}
	getFloat := func(v interface{}) (float64, bool) {
		switch x := v.(type) {
		case float64:
			return x, true
		case float32:
			return float64(x), true
		case int:
			return float64(x), true
		case int64:
			return float64(x), true
		case string:
			if x == "" {
				return 0, true
			}
			d, err := decimal.NewFromString(x)
			if err == nil {
				f, _ := d.Float64()
				return f, true
			}
		}
		return 0, false
	}
	getInt := func(v interface{}) (int, bool) {
		switch x := v.(type) {
		case float64:
			return int(x), true
		case int:
			return x, true
		case int64:
			return int(x), true
		case string:
			if x == "" {
				return 0, true
			}
			var i int
			if _, err := fmt.Sscanf(x, "%d", &i); err == nil {
				return i, true
			}
		}
		return 0, false
	}
	getDate := func(v interface{}) (*time.Time, bool) {
		s, ok := getStr(v)
		if !ok {
			return nil, false
		}
		if s == "" {
			return nil, true // null explicit
		}
		// Try common formats: RFC3339, YYYY-MM-DD, YYYY-MM-DDTHH:MM:SS
		layouts := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, s); err == nil {
				return &t, true
			}
		}
		return nil, false
	}

	for key, raw := range payload {
		switch key {
		case "ncf":
			if s, ok := getStr(raw); ok {
				inv.NCF = s
				applied++
			}
		case "tipo_ncf":
			if s, ok := getStr(raw); ok {
				inv.TipoNCF = s
				applied++
			}
		case "ncf_modifica":
			if s, ok := getStr(raw); ok {
				inv.NCFModifica = s
				applied++
			}
		case "emisor_rnc":
			if s, ok := getStr(raw); ok {
				inv.EmisorRNC = s
				applied++
			}
		case "emisor_nombre", "proveedor":
			if s, ok := getStr(raw); ok {
				inv.Proveedor = s
				applied++
			}
		case "receptor_nombre":
			if s, ok := getStr(raw); ok {
				inv.ReceptorNombre = s
				applied++
			}
		case "receptor_rnc":
			if s, ok := getStr(raw); ok {
				inv.ReceptorRNC = s
				applied++
			}
		case "fecha_emision", "fecha_documento":
			if t, ok := getDate(raw); ok {
				inv.FechaDocumento = t
				applied++
			}
		case "fecha_pago":
			if t, ok := getDate(raw); ok {
				inv.FechaPago = t
				applied++
			}
		case "total_factura", "monto":
			if f, ok := getFloat(raw); ok {
				inv.Monto = f
				applied++
			}
		case "subtotal":
			if f, ok := getFloat(raw); ok {
				inv.Subtotal = f
				applied++
			}
		case "monto_servicios":
			if f, ok := getFloat(raw); ok {
				inv.MontoServicios = f
				applied++
			}
		case "monto_bienes":
			if f, ok := getFloat(raw); ok {
				inv.MontoBienes = f
				applied++
			}
		case "descuento":
			if f, ok := getFloat(raw); ok {
				inv.Descuento = f
				applied++
			}
		case "itbis", "itbis_facturado":
			if f, ok := getFloat(raw); ok {
				inv.ITBIS = f
				applied++
			}
		case "itbis_retenido":
			if f, ok := getFloat(raw); ok {
				inv.ITBISRetenido = f
				applied++
			}
		case "itbis_exento":
			if f, ok := getFloat(raw); ok {
				inv.ITBISExento = f
				applied++
			}
		case "itbis_tasa":
			if f, ok := getFloat(raw); ok {
				inv.ITBISTasa = f
				applied++
			}
		case "itbis_retenido_porcentaje":
			if i, ok := getInt(raw); ok {
				inv.ITBISRetenidoPorcentaje = i
				applied++
			}
		case "isc", "isc_monto":
			if f, ok := getFloat(raw); ok {
				inv.ISC = f
				applied++
			}
		case "isc_categoria":
			if s, ok := getStr(raw); ok {
				inv.ISCCategoria = s
				applied++
			}
		case "propina", "propina_legal":
			if f, ok := getFloat(raw); ok {
				inv.Propina = f
				applied++
			}
		case "otros_impuestos":
			if f, ok := getFloat(raw); ok {
				inv.OtrosImpuestos = f
				applied++
			}
		case "isr", "retencion_isr_monto":
			if f, ok := getFloat(raw); ok {
				inv.ISR = f
				applied++
			}
		case "retencion_isr_tipo":
			if i, ok := getInt(raw); ok {
				inv.RetencionISRTipo = &i
				applied++
			}
		case "extraction_status":
			if s, ok := getStr(raw); ok {
				inv.ExtractionStatus = s
				applied++
			}
		case "review_notes":
			if s, ok := getStr(raw); ok {
				inv.ReviewNotes = s
				applied++
			}
		case "forma_pago":
			if s, ok := getStr(raw); ok {
				inv.FormaPago = s
				applied++
			}
		case "tipo_bien_servicio":
			if s, ok := getStr(raw); ok {
				inv.TipoBienServicio = s
				applied++
			}
		case "estado":
			if s, ok := getStr(raw); ok {
				inv.Estado = s
				applied++
			}
		}
	}
	return applied
}

// UpdateClientInvoiceHandler — PUT /api/facturas/{id}/update
// Actualiza factura en facturas_clientes con triggers BD activos.
// Auth: cliente JWT — sólo puede modificar sus propias facturas (filtro por
// claims.UserID en helper UpdateClientInvoice).
func (h *Handler) UpdateClientInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	ctx := r.Context()

	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		sendAppError(w, ErrDBUnavailable)
		return
	}

	vars := mux.Vars(r)
	invoiceID := vars["id"]
	if invoiceID == "" {
		h.sendError(w, http.StatusBadRequest, "missing invoice id")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Cargar factura existente — UpdateClientInvoice helper requiere struct completo.
	// El SELECT también valida ownership (cliente_id = claims.UserID).
	inv, err := db.GetClientInvoiceByID(ctx, claims.UserID, invoiceID)
	if err != nil {
		log.Printf("[UpdateClientInvoiceHandler] not found cliente=%s invoice=%s: %v", claims.UserID, invoiceID, err)
		h.sendError(w, http.StatusNotFound, "invoice not found")
		return
	}

	applied := applyClientInvoicePayload(inv, payload)
	if applied == 0 {
		h.sendError(w, http.StatusBadRequest, "no allowed fields in payload")
		return
	}

	if err := db.UpdateClientInvoice(ctx, claims.UserID, invoiceID, inv); err != nil {
		log.Printf("[UpdateClientInvoiceHandler] update error cliente=%s invoice=%s: %v", claims.UserID, invoiceID, err)
		h.sendError(w, http.StatusInternalServerError, "failed to update invoice")
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"invoice_id": invoiceID,
		"updated":    applied,
	})
}

// ApproveClientInvoiceHandler — PUT /api/facturas/{id}/approve
// Marca factura como validated. Acepta mismo payload que update + fuerza
// extraction_status='validated' antes de delegar al handler de update.
func (h *Handler) ApproveClientInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	// Leer body (puede venir vacío en aprobaciones puras)
	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()

	payload := map[string]interface{}{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			w.Header().Set("Content-Type", "application/json")
			h.sendError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	// Forzar extraction_status='validated' en todos los casos
	payload["extraction_status"] = "validated"

	newBody, _ := json.Marshal(payload)
	r.Body = io.NopCloser(bytes.NewReader(newBody))
	r.ContentLength = int64(len(newBody))

	h.UpdateClientInvoiceHandler(w, r)
}
