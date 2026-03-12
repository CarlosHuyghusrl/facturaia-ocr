package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/facturaIA/invoice-ocr-service/internal/auth"
	"github.com/facturaIA/invoice-ocr-service/internal/db"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// fmtMonto formats a float64 as a string with exactly 2 decimal places.
// Zero is represented as "" (empty) per DGII 606 spec for optional fields.
// Required fields (monto total, itbis, etc.) always receive the non-empty form.
func fmtMonto(f float64, required bool) string {
	if f == 0 && !required {
		return ""
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

// fmtFecha converts *time.Time → YYYYMMDD, or "" if nil/zero.
func fmtFecha(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("20060102")
}

// tipoIDFromRNC infers DGII tipo_identificacion: "1" for RNC (9 digits), "2" for Cédula (11 digits).
func tipoIDFromRNC(rnc string) string {
	clean := strings.ReplaceAll(rnc, "-", "")
	switch len(clean) {
	case 9:
		return "1"
	case 11:
		return "2"
	default:
		return "1" // default RNC
	}
}

// cleanRNC removes dashes from a RNC/cédula string.
func cleanRNC(rnc string) string {
	return strings.ReplaceAll(rnc, "-", "")
}

// ─────────────────────────────────────────────────────────────────────────────
// 606 Line builder
// ─────────────────────────────────────────────────────────────────────────────

// build606Line produces the 23-field pipe-delimited data line for a single invoice.
// Fields (DGII Formato 606):
// 1  RNC_PROVEEDOR
// 2  TIPO_IDENTIFICACION
// 3  TIPO_BIEN_SERVICIO_ADQUIRIDO
// 4  NUMERO_COMPROBANTE_FISCAL
// 5  NUMERO_COMPROBANTE_MODIFICADO
// 6  FECHA_COMPROBANTE
// 7  FECHA_PAGO
// 8  MONTO_FACTURADO_SERVICIOS
// 9  MONTO_FACTURADO_BIENES
// 10 TOTAL_MONTO_FACTURADO  (subtotal — required)
// 11 ITBIS_FACTURADO
// 12 ITBIS_RETENIDO_TERCERO
// 13 ITBIS_PERCIBIDO_TERCERO
// 14 TIPO_RETENCION_ISR
// 15 RETENCION_RENTA_TERCERO
// 16 ISR_PERCIBIDO_TERCERO
// 17 IMPUESTO_SELECTIVO_CONSUMO
// 18 OTROS_IMPUESTOS_TASAS
// 19 MONTO_PROPINA_LEGAL
// 20 FORMA_PAGO_1  (first two chars, e.g. "04")
// (Note: DGII spec fields 14/15/16 differ slightly by version—we follow v2 order)

// build606Line generates the pipe-delimited 606 data line for one invoice.
// Field order per DGII Formato 606 Rev 2 (23 fields, indices 1-23):
//  1 RNC_PROV  2 TIPO_ID  3 TIPO_BIEN  4 NCF  5 NCF_MOD
//  6 FECHA_COMP  7 FECHA_PAGO  8 MONTO_SERV  9 MONTO_BIENES  10 TOTAL
// 11 ITBIS_FACT  12 ITBIS_RET  13 ITBIS_PROP  14 ITBIS_COSTO  15 ITBIS_ADELANTAR
// 16 ITBIS_PERC  17 RET_ISR_TIPO  18 RET_ISR_MONTO  19 ISR_PERC
// 20 ISC  21 OTROS_IMPTOS  22 PROPINA  23 FORMA_PAGO
func build606Line(inv db.Formato606Invoice) string {
	// Field 1: RNC emisor (clean, no dashes)
	rncProv := cleanRNC(inv.EmisorRNC)

	// Field 2: Tipo ID — use stored value or calculate from RNC length
	tipoID := inv.TipoIDEmisor
	if tipoID == "" {
		tipoID = tipoIDFromRNC(rncProv)
	}

	// Field 3: Tipo bien/servicio — fallback "06" (Bienes y Servicios)
	tipoBien := inv.TipoBienServicio
	if tipoBien == "" {
		tipoBien = "06"
	}

	// Fields 4-5: NCF and NCF Modifica
	ncf := inv.NCF
	ncfMod := inv.NCFModifica

	// Fields 6-7: Dates
	fechaComp := fmtFecha(inv.FechaDocumento)
	fechaPago := fmtFecha(inv.FechaPago)

	// Fields 8-9: Monto servicios / bienes
	montoServ := fmtMonto(inv.MontoServicios, false)
	montoBienes := fmtMonto(inv.MontoBienes, false)

	// Field 10: Total monto facturado (required)
	total := inv.MontoServicios + inv.MontoBienes
	if total == 0 {
		total = inv.Subtotal
	}
	totalStr := fmtMonto(total, true)

	// Field 11: ITBIS facturado
	itbisFact := fmtMonto(inv.ITBIS, false)

	// Field 12: ITBIS retenido
	itbisRet := fmtMonto(inv.ITBISRetenido, false)

	// Field 13: ITBIS proporcionalidad (Art. 349)
	itbisProp := fmtMonto(inv.ITBISProporcionalidad, false)

	// Field 14: ITBIS costo (no deducible)
	itbisCosto := fmtMonto(inv.ITBISCosto, false)

	// Field 15: ITBIS a adelantar = ITBIS - retenido - proporcionalidad - costo
	itbisAdelantar := inv.ITBIS - inv.ITBISRetenido - inv.ITBISProporcionalidad - inv.ITBISCosto
	if itbisAdelantar < 0 {
		itbisAdelantar = 0
	}
	itbisAdelStr := fmtMonto(itbisAdelantar, false)

	// Field 16: ITBIS percibido
	itbisPerc := fmtMonto(inv.ITBISPercibido, false)

	// Field 17: Retención ISR tipo
	retISRTipo := ""
	if inv.RetencionISRTipo != nil && *inv.RetencionISRTipo > 0 {
		retISRTipo = strconv.Itoa(*inv.RetencionISRTipo)
	}

	// Field 18: Retención ISR monto
	retISRMonto := fmtMonto(inv.ISR, false)

	// Field 19: ISR percibido
	isrPerc := fmtMonto(inv.ISRPercibido, false)

	// Field 20: ISC
	iscStr := fmtMonto(inv.ISC, false)

	// Field 21: Otros impuestos = CDT + 911
	otrosImp := inv.CDTMonto + inv.Cargo911
	otrosImpStr := fmtMonto(otrosImp, false)

	// Field 22: Propina legal
	propinaStr := fmtMonto(inv.Propina, false)

	// Field 23: Forma de pago (numeric code, first 2 chars)
	formaPago := inv.FormaPago
	if len(formaPago) > 2 {
		formaPago = formaPago[:2]
	}

	parts := []string{
		rncProv, tipoID, tipoBien, ncf, ncfMod,
		fechaComp, fechaPago, montoServ, montoBienes, totalStr,
		itbisFact, itbisRet, itbisProp, itbisCosto, itbisAdelStr,
		itbisPerc, retISRTipo, retISRMonto, isrPerc,
		iscStr, otrosImpStr, propinaStr, formaPago,
	}
	return strings.Join(parts, "|")
}

// ─────────────────────────────────────────────────────────────────────────────
// 606 Validation helpers
// ─────────────────────────────────────────────────────────────────────────────

type validationResult606 struct {
	Errores      []string `json:"errores"`
	Advertencias []string `json:"advertencias"`
}

func validate606Invoice(inv db.Formato606Invoice, idx int) validationResult606 {
	var res validationResult606
	prefix := fmt.Sprintf("Registro %d", idx+1)

	if cleanRNC(inv.EmisorRNC) == "" {
		res.Errores = append(res.Errores, fmt.Sprintf("%s: RNC emisor vacío", prefix))
	}
	if inv.NCF == "" {
		res.Advertencias = append(res.Advertencias, fmt.Sprintf("%s: Sin NCF", prefix))
	}
	if inv.FechaDocumento == nil || inv.FechaDocumento.IsZero() {
		res.Errores = append(res.Errores, fmt.Sprintf("%s: Fecha documento vacía", prefix))
	}
	total := inv.MontoServicios + inv.MontoBienes
	if total == 0 {
		total = inv.Subtotal
	}
	if total == 0 {
		res.Errores = append(res.Errores, fmt.Sprintf("%s: Monto total es 0", prefix))
	}
	return res
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/formato-606/{rnc_receptor}?periodo=YYYYMM  (download TXT)
// ─────────────────────────────────────────────────────────────────────────────

func (h *Handler) GetFormato606(w http.ResponseWriter, r *http.Request) {
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
	rncReceptor := cleanRNC(vars["rnc_receptor"])
	periodo := r.URL.Query().Get("periodo")
	if len(periodo) != 6 {
		h.sendError(w, http.StatusBadRequest, "periodo requerido en formato YYYYMM")
		return
	}

	invoices, err := db.GetFormato606Invoices(ctx, rncReceptor, periodo)
	if err != nil {
		log.Printf("GetFormato606: DB error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "error consultando facturas")
		return
	}

	// Build TXT content
	// Cabecera: 606|{rnc}|{periodo}|{cantidad}
	lines := make([]string, 0, len(invoices)+1)
	lines = append(lines, fmt.Sprintf("606|%s|%s|%d", rncReceptor, periodo, len(invoices)))

	var totalMonto, totalITBIS, totalAdelantar float64
	for _, inv := range invoices {
		lines = append(lines, build606Line(inv))
		total := inv.MontoServicios + inv.MontoBienes
		if total == 0 {
			total = inv.Subtotal
		}
		totalMonto += total
		totalITBIS += inv.ITBIS
		itbisAdel := inv.ITBIS - inv.ITBISRetenido - inv.ITBISProporcionalidad - inv.ITBISCosto
		if itbisAdel > 0 {
			totalAdelantar += itbisAdel
		}
	}

	contenido := strings.Join(lines, "\n") + "\n"
	filename := fmt.Sprintf("DGII_F_606_%s_%s.TXT", rncReceptor, periodo)

	// Save to envios_606 (non-blocking on error — still deliver the file)
	go func() {
		_, insErr := db.InsertEnvio606(ctx, claims.UserID, rncReceptor, periodo, contenido, filename,
			len(invoices), totalMonto, totalITBIS, totalAdelantar)
		if insErr != nil {
			log.Printf("GetFormato606: InsertEnvio606 error: %v", insErr)
		}
	}()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, contenido)
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/formato-606/{rnc_receptor}/preview?periodo=YYYYMM
// ─────────────────────────────────────────────────────────────────────────────

type Formato606PreviewDetail struct {
	ID           string  `json:"id"`
	RNCEmisor    string  `json:"rnc_emisor"`
	Proveedor    string  `json:"proveedor"`
	NCF          string  `json:"ncf"`
	FechaDoc     string  `json:"fecha_documento"`
	Total        float64 `json:"total"`
	ITBIS        float64 `json:"itbis"`
	ITBISAdelantar float64 `json:"itbis_adelantar"`
}

type Formato606PreviewResponse struct {
	RNC              string                    `json:"rnc"`
	Periodo          string                    `json:"periodo"`
	Registros        int                       `json:"registros"`
	TotalFacturado   float64                   `json:"total_facturado"`
	ITBISFacturado   float64                   `json:"itbis_facturado"`
	ITBISPorAdelantar float64                  `json:"itbis_por_adelantar"`
	Errores          []string                  `json:"errores"`
	Advertencias     []string                  `json:"advertencias"`
	Detalle          []Formato606PreviewDetail `json:"detalle"`
}

func (h *Handler) GetFormato606Preview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	ctx := r.Context()

	_, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		sendAppError(w, ErrDBUnavailable)
		return
	}

	vars := mux.Vars(r)
	rncReceptor := cleanRNC(vars["rnc_receptor"])
	periodo := r.URL.Query().Get("periodo")
	if len(periodo) != 6 {
		h.sendError(w, http.StatusBadRequest, "periodo requerido en formato YYYYMM")
		return
	}

	invoices, err := db.GetFormato606Invoices(ctx, rncReceptor, periodo)
	if err != nil {
		log.Printf("GetFormato606Preview: DB error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "error consultando facturas")
		return
	}

	resp := Formato606PreviewResponse{
		RNC:          rncReceptor,
		Periodo:      periodo,
		Registros:    len(invoices),
		Errores:      []string{},
		Advertencias: []string{},
		Detalle:      make([]Formato606PreviewDetail, 0, len(invoices)),
	}

	for i, inv := range invoices {
		total := inv.MontoServicios + inv.MontoBienes
		if total == 0 {
			total = inv.Subtotal
		}
		itbisAdel := inv.ITBIS - inv.ITBISRetenido - inv.ITBISProporcionalidad - inv.ITBISCosto
		if itbisAdel < 0 {
			itbisAdel = 0
		}

		resp.TotalFacturado += total
		resp.ITBISFacturado += inv.ITBIS
		resp.ITBISPorAdelantar += itbisAdel

		// Validate each line
		vr := validate606Invoice(inv, i)
		resp.Errores = append(resp.Errores, vr.Errores...)
		resp.Advertencias = append(resp.Advertencias, vr.Advertencias...)

		fechaStr := fmtFecha(inv.FechaDocumento)
		resp.Detalle = append(resp.Detalle, Formato606PreviewDetail{
			ID:             inv.ID,
			RNCEmisor:      cleanRNC(inv.EmisorRNC),
			Proveedor:      inv.Proveedor,
			NCF:            inv.NCF,
			FechaDoc:       fechaStr,
			Total:          total,
			ITBIS:          inv.ITBIS,
			ITBISAdelantar: itbisAdel,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: POST /api/formato-606/{rnc_receptor}/validate?periodo=YYYYMM
// ─────────────────────────────────────────────────────────────────────────────

func (h *Handler) ValidateFormato606(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	ctx := r.Context()

	_, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		sendAppError(w, ErrDBUnavailable)
		return
	}

	vars := mux.Vars(r)
	rncReceptor := cleanRNC(vars["rnc_receptor"])
	periodo := r.URL.Query().Get("periodo")
	if len(periodo) != 6 {
		h.sendError(w, http.StatusBadRequest, "periodo requerido en formato YYYYMM")
		return
	}

	invoices, err := db.GetFormato606Invoices(ctx, rncReceptor, periodo)
	if err != nil {
		log.Printf("ValidateFormato606: DB error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "error consultando facturas")
		return
	}

	var allErrors, allWarnings []string
	for i, inv := range invoices {
		vr := validate606Invoice(inv, i)
		allErrors = append(allErrors, vr.Errores...)
		allWarnings = append(allWarnings, vr.Advertencias...)
	}

	if allErrors == nil {
		allErrors = []string{}
	}
	if allWarnings == nil {
		allWarnings = []string{}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"rnc":          rncReceptor,
		"periodo":      periodo,
		"registros":    len(invoices),
		"valido":       len(allErrors) == 0,
		"errores":      allErrors,
		"advertencias": allWarnings,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: PUT /api/formato-606/factura/{id}/toggle-aplica606
// ─────────────────────────────────────────────────────────────────────────────

func (h *Handler) ToggleAplica606(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Aplica606 bool `json:"aplica_606"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := db.ToggleAplica606(ctx, claims.UserID, invoiceID, body.Aplica606); err != nil {
		log.Printf("ToggleAplica606: DB error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "error actualizando factura")
		return
	}

	// Return updated invoice
	inv, err := db.GetClientInvoiceByID(ctx, claims.UserID, invoiceID)
	if err != nil {
		// Not a hard error — just return success without the invoice
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"aplica_606": body.Aplica606,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"factura": clientInvoiceToFrontend(inv),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: PUT /api/envios-606/{id}/referencia
// ─────────────────────────────────────────────────────────────────────────────

func (h *Handler) UpdateEnvio606Referencia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	ctx := r.Context()

	_, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if db.Pool == nil {
		sendAppError(w, ErrDBUnavailable)
		return
	}

	vars := mux.Vars(r)
	envioID := vars["id"]

	var body struct {
		ReferenciaDGII string `json:"referencia_dgii"`
		EstadoEnvio    string `json:"estatus_envio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	estado := body.EstadoEnvio
	if estado == "" {
		estado = "enviado"
	}

	if err := db.UpdateEnvio606Referencia(ctx, envioID, body.ReferenciaDGII, estado); err != nil {
		log.Printf("UpdateEnvio606Referencia: DB error: %v", err)
		h.sendError(w, http.StatusInternalServerError, "error actualizando envío")
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":         true,
		"referencia_dgii": body.ReferenciaDGII,
		"estado":          estado,
		"fecha_envio":     time.Now().Format(time.RFC3339),
	})
}
