package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/minio/minio-go/v7"

	"github.com/facturaIA/invoice-ocr-service/internal/auth"
	"github.com/facturaIA/invoice-ocr-service/internal/db"
	"github.com/facturaIA/invoice-ocr-service/internal/storage"
)

// GetClientInvoices - GET /api/facturas/mis-facturas/
func (h *Handler) GetClientInvoices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims, err := auth.GetClaimsFromContext(r.Context())
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	// Parse pagination params
	page := 1
	limit := 50
	if p := r.URL.Query().Get("page"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			page = val
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= 100 {
			limit = val
		}
	}

	offset := (page - 1) * limit

	invoices, total, err := db.GetClientInvoicesPaginated(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		log.Printf("GetClientInvoices error for user %s: %v", claims.UserID, err)
		h.sendError(w, http.StatusInternalServerError, "failed to get invoices")
		return
	}

	if invoices == nil {
		invoices = []db.ClientInvoice{}
	}

	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}

	// Map to frontend format
	mapped := make([]map[string]interface{}, len(invoices))
	for i, inv := range invoices {
		mapped[i] = clientInvoiceToFrontend(&inv)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"facturas":    mapped,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": totalPages,
	})
}

// GetClientStats - GET /api/facturas/resumen
func (h *Handler) GetClientStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims, err := auth.GetClaimsFromContext(r.Context())
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		h.sendError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	stats, err := db.GetClientStats(r.Context(), claims.UserID)
	if err != nil {
		log.Printf("GetClientStats error for user %s: %v", claims.UserID, err)
		h.sendError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	// Get monthly stats too
	monthStats, _ := db.GetClientMonthlyStats(r.Context(), claims.UserID)
	facturasMes := 0
	itbisMes := 0.0
	totalMes := 0.0
	errores := 0
	if monthStats != nil {
		facturasMes = monthStats.TotalFacturas
		totalMes = monthStats.MontoTotal
		itbisMes = monthStats.ITBISTotal
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"resumen": map[string]interface{}{
			"total_facturas": stats.TotalFacturas,
			"facturas_mes":   facturasMes,
			"itbis_mes":      itbisMes,
			"total_mes":      totalMes,
			"pendientes":     stats.Pendientes,
			"procesadas":     stats.Procesadas,
			"errores":        errores,
		},
	})
}

// GetClientInvoice - GET /api/facturas/{id}
func (h *Handler) GetClientInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims, err := auth.GetClaimsFromContext(r.Context())
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

	invoice, err := db.GetClientInvoiceByID(r.Context(), claims.UserID, invoiceID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "invoice not found")
		return
	}

	// Map ClientInvoice to frontend Factura interface
	json.NewEncoder(w).Encode(map[string]interface{}{
		"factura": clientInvoiceToFrontend(invoice),
	})
}

// DeleteClientInvoice - DELETE /api/facturas/{id}
func (h *Handler) DeleteClientInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims, err := auth.GetClaimsFromContext(r.Context())
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

	if err := db.DeleteClientInvoice(r.Context(), claims.UserID, invoiceID); err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to delete invoice")
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "factura eliminada",
	})
}

// ReprocesarClientInvoice - POST /api/facturas/{id}/reprocesar
func (h *Handler) ReprocesarClientInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims, err := auth.GetClaimsFromContext(r.Context())
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

	// Get existing invoice
	invoice, err := db.GetClientInvoiceByID(r.Context(), claims.UserID, invoiceID)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "invoice not found")
		return
	}

	// Download image from MinIO
	if storage.Client == nil {
		h.sendError(w, http.StatusServiceUnavailable, "storage not available")
		return
	}

	// Remove bucket prefix to get object name
	objectName := invoice.ArchivoURL
	prefix := storage.BucketName + "/"
	if strings.HasPrefix(objectName, prefix) {
		objectName = objectName[len(prefix):]
	}

	obj, err := storage.Client.GetObject(r.Context(), storage.BucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("ReprocesarClientInvoice: MinIO error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "failed to retrieve image from storage")
		return
	}
	defer obj.Close()

	// Read image bytes
	imageData, err := io.ReadAll(obj)
	if err != nil {
		log.Printf("ReprocesarClientInvoice: Read error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "failed to read image")
		return
	}

	// Reprocess with AI
	fmt.Printf("[Reprocesar] Reprocesando factura %s con AI\n", invoiceID)
	reprocessedInvoice, _, _, _, err := h.processInvoice(
		imageData,
		true, // useVisionModel
		h.config.AI.DefaultProvider,
		"",
		h.config.OCR.Language,
	)

	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "OCR reprocessing failed: "+err.Error())
		return
	}

	// Parse fecha
	var fechaDoc *time.Time
	if !reprocessedInvoice.FechaFactura.IsZero() {
		t := reprocessedInvoice.FechaFactura
		fechaDoc = &t
	} else if !reprocessedInvoice.Date.IsZero() {
		t := reprocessedInvoice.Date
		fechaDoc = &t
	}

	// Build OCR notes summary
	ocrNotes := ""
	if ocrJSON, err := json.Marshal(reprocessedInvoice); err == nil {
		ocrNotes = string(ocrJSON)
	}

	// Serialize items to JSON
	itemsJSON := ""
	if len(reprocessedInvoice.Items) > 0 {
		if ij, err := json.Marshal(reprocessedInvoice.Items); err == nil {
			itemsJSON = string(ij)
		}
	}

	// Build updated invoice
	updatedInvoice := &db.ClientInvoice{
		NCF:              reprocessedInvoice.NCF,
		TipoNCF:          reprocessedInvoice.TipoNCF,
		EmisorRNC:        reprocessedInvoice.RNCEmisor,
		Proveedor:        reprocessedInvoice.NombreEmisor,
		ReceptorNombre:   reprocessedInvoice.NombreReceptor,
		ReceptorRNC:      reprocessedInvoice.RNCReceptor,
		FechaDocumento:   fechaDoc,
		Monto:            decimalToFloat64(reprocessedInvoice.Total),
		Subtotal:         decimalToFloat64(reprocessedInvoice.Subtotal),
		Descuento:        decimalToFloat64(reprocessedInvoice.Descuento),
		ITBIS:            decimalToFloat64(reprocessedInvoice.ITBIS),
		ITBISRetenido:    decimalToFloat64(reprocessedInvoice.ITBISRetenido),
		ITBISExento:      decimalToFloat64(reprocessedInvoice.ITBISExento),
		ITBISProporcionalidad: decimalToFloat64(reprocessedInvoice.ITBISProporcionalidad),
		ITBISCosto:       decimalToFloat64(reprocessedInvoice.ITBISCosto),
		ISR:              decimalToFloat64(reprocessedInvoice.ISR),
		RetencionISRTipo: intToPtr(reprocessedInvoice.RetencionISRTipo),
		ISC:              decimalToFloat64(reprocessedInvoice.ISC),
		ISCCategoria:     reprocessedInvoice.ISCCategoria,
		CDTMonto:         decimalToFloat64(reprocessedInvoice.CDTMonto),
		Cargo911:         decimalToFloat64(reprocessedInvoice.Cargo911),
		Propina:          decimalToFloat64(reprocessedInvoice.Propina),
		OtrosImpuestos:   decimalToFloat64(reprocessedInvoice.OtrosImpuestos),
		MontoNoFacturable: decimalToFloat64(reprocessedInvoice.MontoNoFacturable),
		FormaPago:        reprocessedInvoice.FormaPago,
		TipoBienServicio: reprocessedInvoice.TipoBienServicio,
		ConfidenceScore:  reprocessedInvoice.Confidence,
		RawOCRJSON:       ocrNotes,
		ItemsJSON:        itemsJSON,
		ExtractionStatus: "validated",
		ReviewNotes:      "",
		Estado:           "procesado",
	}

	// Update in database
	if err := db.UpdateClientInvoice(r.Context(), claims.UserID, invoiceID, updatedInvoice); err != nil {
		log.Printf("ReprocesarClientInvoice: DB update error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "failed to update invoice in database")
		return
	}

	// Get updated invoice to return
	finalInvoice, err := db.GetClientInvoiceByID(r.Context(), claims.UserID, invoiceID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "failed to retrieve updated invoice")
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"factura": clientInvoiceToFrontend(finalInvoice),
		"message": "factura reprocesada exitosamente",
	})
}

// GetClientInvoiceImage - GET /api/facturas/{id}/imagen - Proxy MinIO image
// No JWT required - protected by UUID (not guessable)
func (h *Handler) GetClientInvoiceImage(w http.ResponseWriter, r *http.Request) {
	if storage.Client == nil {
		http.Error(w, "storage not available", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	invoiceID := vars["id"]

	if db.Pool == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var archivoURL string
	err := db.Pool.QueryRow(r.Context(),
		"SELECT COALESCE(archivo_url, '') FROM facturas_clientes WHERE id = $1::uuid", invoiceID,
	).Scan(&archivoURL)
	if err != nil || archivoURL == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Remove bucket prefix to get object name
	objectName := archivoURL
	prefix := storage.BucketName + "/"
	if strings.HasPrefix(objectName, prefix) {
		objectName = objectName[len(prefix):]
	}

	obj, err := storage.Client.GetObject(r.Context(), storage.BucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("GetClientInvoiceImage: MinIO error: %v", err)
		http.Error(w, "image not available", http.StatusInternalServerError)
		return
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		log.Printf("GetClientInvoiceImage: Stat error: %v", err)
		http.Error(w, "image not available", http.StatusInternalServerError)
		return
	}

	contentType := info.ContentType
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	io.Copy(w, obj)
}

// clientInvoiceToFrontend maps ClientInvoice DB fields to frontend Factura interface
func clientInvoiceToFrontend(inv *db.ClientInvoice) map[string]interface{} {
	fechaEmision := ""
	if inv.FechaDocumento != nil {
		fechaEmision = inv.FechaDocumento.Format("2006-01-02")
	}

	return map[string]interface{}{
		"id":                  inv.ID,
		"cliente_id":          inv.ClienteID,
		"ncf":                 inv.NCF,
		"tipo_ncf":            inv.TipoNCF,
		"tipo_comprobante":    inv.TipoNCF,
		"emisor_rnc":          inv.EmisorRNC,
		"emisor_nombre":       inv.Proveedor,
		"proveedor":           inv.Proveedor,
		"receptor_rnc":        inv.ReceptorRNC,
		"receptor_nombre":     inv.ReceptorNombre,
		"fecha_emision":       fechaEmision,
		"fecha_documento":     fechaEmision,
		// Montos base
		"subtotal":            inv.Subtotal,
		"descuento":           inv.Descuento,
		"monto":               inv.Monto,
		"total":               inv.Monto,
		// ITBIS
		"itbis":                   inv.ITBIS,
		"itbis_retenido":          inv.ITBISRetenido,
		"itbis_exento":            inv.ITBISExento,
		"itbis_proporcionalidad":  inv.ITBISProporcionalidad,
		"itbis_costo":             inv.ITBISCosto,
		// ISR
		"isr":                 inv.ISR,
		"retencion_isr_tipo":  inv.RetencionISRTipo,
		// ISC
		"isc":                 inv.ISC,
		"isc_categoria":       inv.ISCCategoria,
		// Otros cargos
		"cdt_monto":           inv.CDTMonto,
		"cargo_911":           inv.Cargo911,
		"propina":             inv.Propina,
		"otros_impuestos":     inv.OtrosImpuestos,
		"monto_no_facturable": inv.MontoNoFacturable,
		// Estado y metadata
		"estado":              inv.Estado,
		"estado_ocr":          "completado",
		"imagen_url":          fmt.Sprintf("/api/facturas/%s/imagen", inv.ID),
		"confidence_score":    inv.ConfidenceScore,
		"forma_pago":          inv.FormaPago,
		"tipo_bien_servicio":  inv.TipoBienServicio,
		"notas_cliente":       inv.NotasCliente,
		"notas_contador":      inv.NotasContador,
		"created_at":          inv.CreatedAt,
	}
}
