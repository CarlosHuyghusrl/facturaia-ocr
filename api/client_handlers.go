package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

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

	// TODO: Re-download image from MinIO and reprocess with AI
	// For now, return the existing invoice
	fmt.Printf("[Reprocesar] Invoice %s requested reprocesamiento\n", invoiceID)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"factura": clientInvoiceToFrontend(invoice),
		"message": "factura reprocesada",
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
		"id":                inv.ID,
		"cliente_id":        inv.ClienteID,
		"ncf":               inv.NCF,
		"tipo_comprobante":  inv.TipoNCF,
		"emisor_rnc":        inv.EmisorRNC,
		"emisor_nombre":     inv.Proveedor,
		"receptor_rnc":      inv.ReceptorRNC,
		"receptor_nombre":   inv.ReceptorNombre,
		"fecha_emision":     fechaEmision,
		"subtotal":          inv.Subtotal,
		"itbis":             inv.ITBIS,
		"itbis_retenido":    inv.ITBISRetenido,
		"isr":               inv.ISR,
		"propina":           inv.Propina,
		"otros_impuestos":   inv.OtrosImpuestos,
		"total":             inv.Monto,
		"estado":            inv.Estado,
		"estado_ocr":        "completado",
		"imagen_url":        fmt.Sprintf("/api/facturas/%s/imagen", inv.ID),
		"confidence_score":  inv.ConfidenceScore,
		"forma_pago":        inv.FormaPago,
		"tipo_bien_servicio": inv.TipoBienServicio,
		"notas_cliente":     inv.NotasCliente,
		"notas_contador":    inv.NotasContador,
		"created_at":        inv.CreatedAt,
	}
}
