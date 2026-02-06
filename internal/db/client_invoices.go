package db

import (
	"context"
	"errors"
	"time"
)

var ErrNoDatabase = errors.New("database not available")

// ClientInvoice - Factura para clientes (facturas_clientes)
type ClientInvoice struct {
	ID             string     `json:"id"`
	ClienteID      string     `json:"cliente_id"`
	EmpresaID      *string    `json:"empresa_id,omitempty"`
	ArchivoURL     string     `json:"archivo_url,omitempty"`
	ArchivoNombre  string     `json:"archivo_nombre,omitempty"`
	ArchivoSize    int        `json:"archivo_size,omitempty"`
	TipoDocumento  string     `json:"tipo_documento,omitempty"`
	FechaDocumento *time.Time `json:"fecha_documento,omitempty"`
	Monto          float64    `json:"monto"`
	NCF            string     `json:"ncf,omitempty"`
	Proveedor      string     `json:"proveedor,omitempty"`
	Estado         string     `json:"estado"`
	NotasCliente   string     `json:"notas_cliente,omitempty"`
	NotasContador  string     `json:"notas_contador,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`

	// Campos DGII granulares (extraídos por IA)
	EmisorRNC       string  `json:"emisor_rnc,omitempty"`
	ReceptorNombre  string  `json:"receptor_nombre,omitempty"`
	ReceptorRNC     string  `json:"receptor_rnc,omitempty"`
	Subtotal        float64 `json:"subtotal"`
	ITBIS           float64 `json:"itbis"`
	ITBISRetenido   float64 `json:"itbis_retenido"`
	ISR             float64 `json:"isr"`
	Propina         float64 `json:"propina"`
	OtrosImpuestos  float64 `json:"otros_impuestos"`
	FormaPago       string  `json:"forma_pago,omitempty"`
	TipoNCF         string  `json:"tipo_ncf,omitempty"`
	TipoBienServicio string `json:"tipo_bien_servicio,omitempty"`
	ConfidenceScore float64 `json:"confidence_score"`
	RawOCRJSON      string  `json:"raw_ocr_json,omitempty"`
	ItemsJSON       string  `json:"items_json,omitempty"`
}

// ClientStats - Estadisticas para clientes
type ClientStats struct {
	TotalFacturas int     `json:"total_facturas"`
	Pendientes    int     `json:"pendientes"`
	Procesadas    int     `json:"procesadas"`
	MontoTotal    float64 `json:"monto_total"`
	ITBISTotal    float64 `json:"itbis_total"`
}

// GetClientInvoices - Obtener facturas de un cliente
func GetClientInvoices(ctx context.Context, clienteID string, limit int) ([]ClientInvoice, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}

	query := `
		SELECT id, cliente_id, empresa_id, COALESCE(archivo_url, ''), COALESCE(archivo_nombre, ''),
		       COALESCE(archivo_size, 0), COALESCE(tipo_documento, ''), fecha_documento,
		       COALESCE(monto, 0), COALESCE(ncf, ''), COALESCE(proveedor, ''),
		       COALESCE(estado, 'pendiente'), COALESCE(notas_cliente, ''), COALESCE(notas_contador, ''),
		       created_at,
		       COALESCE(emisor_rnc, ''), COALESCE(receptor_nombre, ''), COALESCE(receptor_rnc, ''),
		       COALESCE(subtotal, 0), COALESCE(itbis, 0), COALESCE(itbis_retenido, 0),
		       COALESCE(isr, 0), COALESCE(propina, 0), COALESCE(otros_impuestos, 0),
		       COALESCE(forma_pago, ''), COALESCE(tipo_ncf, ''), COALESCE(tipo_bien_servicio, ''),
		       COALESCE(confidence_score, 0)
		FROM facturas_clientes
		WHERE cliente_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := Pool.Query(ctx, query, clienteID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invoices []ClientInvoice
	for rows.Next() {
		var inv ClientInvoice
		err := rows.Scan(
			&inv.ID, &inv.ClienteID, &inv.EmpresaID, &inv.ArchivoURL, &inv.ArchivoNombre,
			&inv.ArchivoSize, &inv.TipoDocumento, &inv.FechaDocumento,
			&inv.Monto, &inv.NCF, &inv.Proveedor,
			&inv.Estado, &inv.NotasCliente, &inv.NotasContador,
			&inv.CreatedAt,
			&inv.EmisorRNC, &inv.ReceptorNombre, &inv.ReceptorRNC,
			&inv.Subtotal, &inv.ITBIS, &inv.ITBISRetenido,
			&inv.ISR, &inv.Propina, &inv.OtrosImpuestos,
			&inv.FormaPago, &inv.TipoNCF, &inv.TipoBienServicio,
			&inv.ConfidenceScore,
		)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}

	return invoices, nil
}

// GetClientInvoicesPaginated - Obtener facturas con paginación
func GetClientInvoicesPaginated(ctx context.Context, clienteID string, limit, offset int) ([]ClientInvoice, int, error) {
	if Pool == nil {
		return nil, 0, ErrNoDatabase
	}

	// Count total
	var total int
	countQuery := `SELECT COUNT(*) FROM facturas_clientes WHERE cliente_id = $1::uuid`
	if err := Pool.QueryRow(ctx, countQuery, clienteID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, cliente_id, empresa_id, COALESCE(archivo_url, ''), COALESCE(archivo_nombre, ''),
		       COALESCE(archivo_size, 0), COALESCE(tipo_documento, ''), fecha_documento,
		       COALESCE(monto, 0), COALESCE(ncf, ''), COALESCE(proveedor, ''),
		       COALESCE(estado, 'pendiente'), COALESCE(notas_cliente, ''), COALESCE(notas_contador, ''),
		       created_at,
		       COALESCE(emisor_rnc, ''), COALESCE(receptor_nombre, ''), COALESCE(receptor_rnc, ''),
		       COALESCE(subtotal, 0), COALESCE(itbis, 0), COALESCE(itbis_retenido, 0),
		       COALESCE(isr, 0), COALESCE(propina, 0), COALESCE(otros_impuestos, 0),
		       COALESCE(forma_pago, ''), COALESCE(tipo_ncf, ''), COALESCE(tipo_bien_servicio, ''),
		       COALESCE(confidence_score, 0)
		FROM facturas_clientes
		WHERE cliente_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := Pool.Query(ctx, query, clienteID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var invoices []ClientInvoice
	for rows.Next() {
		var inv ClientInvoice
		err := rows.Scan(
			&inv.ID, &inv.ClienteID, &inv.EmpresaID, &inv.ArchivoURL, &inv.ArchivoNombre,
			&inv.ArchivoSize, &inv.TipoDocumento, &inv.FechaDocumento,
			&inv.Monto, &inv.NCF, &inv.Proveedor,
			&inv.Estado, &inv.NotasCliente, &inv.NotasContador,
			&inv.CreatedAt,
			&inv.EmisorRNC, &inv.ReceptorNombre, &inv.ReceptorRNC,
			&inv.Subtotal, &inv.ITBIS, &inv.ITBISRetenido,
			&inv.ISR, &inv.Propina, &inv.OtrosImpuestos,
			&inv.FormaPago, &inv.TipoNCF, &inv.TipoBienServicio,
			&inv.ConfidenceScore,
		)
		if err != nil {
			return nil, 0, err
		}
		invoices = append(invoices, inv)
	}

	return invoices, total, nil
}

// GetClientMonthlyStats - Estadísticas del mes actual
func GetClientMonthlyStats(ctx context.Context, clienteID string) (*ClientStats, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}

	query := `
		SELECT
		    COUNT(*) as total,
		    COUNT(*) FILTER (WHERE estado = 'pendiente') as pendientes,
		    COUNT(*) FILTER (WHERE estado IN ('procesado', 'procesada', 'completado', 'completada')) as procesadas,
		    COALESCE(SUM(monto), 0) as monto_total,
		    COALESCE(SUM(itbis), 0) as itbis_total
		FROM facturas_clientes
		WHERE cliente_id = $1::uuid
		AND DATE_TRUNC('month', created_at) = DATE_TRUNC('month', CURRENT_DATE)
	`

	var stats ClientStats
	err := Pool.QueryRow(ctx, query, clienteID).Scan(
		&stats.TotalFacturas, &stats.Pendientes, &stats.Procesadas, &stats.MontoTotal, &stats.ITBISTotal,
	)
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

// GetClientStats - Obtener estadisticas de un cliente
func GetClientStats(ctx context.Context, clienteID string) (*ClientStats, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}

	query := `
		SELECT
		    COUNT(*) as total,
		    COUNT(*) FILTER (WHERE estado = 'pendiente') as pendientes,
		    COUNT(*) FILTER (WHERE estado IN ('procesado', 'procesada', 'completado', 'completada')) as procesadas,
		    COALESCE(SUM(monto), 0) as monto_total
		FROM facturas_clientes
		WHERE cliente_id = $1::uuid
	`

	var stats ClientStats
	err := Pool.QueryRow(ctx, query, clienteID).Scan(
		&stats.TotalFacturas, &stats.Pendientes, &stats.Procesadas, &stats.MontoTotal,
	)
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

// GetClientInvoiceByID - Obtener una factura especifica de un cliente
func GetClientInvoiceByID(ctx context.Context, clienteID, invoiceID string) (*ClientInvoice, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}

	query := `
		SELECT id, cliente_id, empresa_id, COALESCE(archivo_url, ''), COALESCE(archivo_nombre, ''),
		       COALESCE(archivo_size, 0), COALESCE(tipo_documento, ''), fecha_documento,
		       COALESCE(monto, 0), COALESCE(ncf, ''), COALESCE(proveedor, ''),
		       COALESCE(estado, 'pendiente'), COALESCE(notas_cliente, ''), COALESCE(notas_contador, ''),
		       created_at,
		       COALESCE(emisor_rnc, ''), COALESCE(receptor_nombre, ''), COALESCE(receptor_rnc, ''),
		       COALESCE(subtotal, 0), COALESCE(itbis, 0), COALESCE(itbis_retenido, 0),
		       COALESCE(isr, 0), COALESCE(propina, 0), COALESCE(otros_impuestos, 0),
		       COALESCE(forma_pago, ''), COALESCE(tipo_ncf, ''), COALESCE(tipo_bien_servicio, ''),
		       COALESCE(confidence_score, 0)
		FROM facturas_clientes
		WHERE cliente_id = $1::uuid AND id = $2::uuid
	`

	var inv ClientInvoice
	err := Pool.QueryRow(ctx, query, clienteID, invoiceID).Scan(
		&inv.ID, &inv.ClienteID, &inv.EmpresaID, &inv.ArchivoURL, &inv.ArchivoNombre,
		&inv.ArchivoSize, &inv.TipoDocumento, &inv.FechaDocumento,
		&inv.Monto, &inv.NCF, &inv.Proveedor,
		&inv.Estado, &inv.NotasCliente, &inv.NotasContador,
		&inv.CreatedAt,
		&inv.EmisorRNC, &inv.ReceptorNombre, &inv.ReceptorRNC,
		&inv.Subtotal, &inv.ITBIS, &inv.ITBISRetenido,
		&inv.ISR, &inv.Propina, &inv.OtrosImpuestos,
		&inv.FormaPago, &inv.TipoNCF, &inv.TipoBienServicio,
		&inv.ConfidenceScore,
	)
	if err != nil {
		return nil, err
	}

	return &inv, nil
}

// SaveClientInvoice - Guardar factura escaneada por cliente en facturas_clientes
func SaveClientInvoice(ctx context.Context, inv *ClientInvoice) error {
	if Pool == nil {
		return ErrNoDatabase
	}

	query := `
		INSERT INTO facturas_clientes (
			cliente_id, archivo_url, archivo_nombre, archivo_size,
			tipo_documento, fecha_documento, monto, ncf, proveedor,
			estado, notas_cliente,
			emisor_rnc, receptor_nombre, receptor_rnc,
			subtotal, itbis, itbis_retenido, isr, propina, otros_impuestos,
			forma_pago, tipo_ncf, tipo_bien_servicio,
			confidence_score, raw_ocr_json, items_json
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23,
			$24, $25::jsonb, $26::jsonb
		)
		RETURNING id, created_at
	`

	// Handle nullable JSONB
	var rawJSON, itemsJSON interface{}
	if inv.RawOCRJSON != "" {
		rawJSON = inv.RawOCRJSON
	}
	if inv.ItemsJSON != "" {
		itemsJSON = inv.ItemsJSON
	}

	err := Pool.QueryRow(ctx, query,
		inv.ClienteID, inv.ArchivoURL, inv.ArchivoNombre, inv.ArchivoSize,
		inv.TipoDocumento, inv.FechaDocumento, inv.Monto, inv.NCF, inv.Proveedor,
		inv.Estado, inv.NotasCliente,
		inv.EmisorRNC, inv.ReceptorNombre, inv.ReceptorRNC,
		inv.Subtotal, inv.ITBIS, inv.ITBISRetenido, inv.ISR, inv.Propina, inv.OtrosImpuestos,
		inv.FormaPago, inv.TipoNCF, inv.TipoBienServicio,
		inv.ConfidenceScore, rawJSON, itemsJSON,
	).Scan(&inv.ID, &inv.CreatedAt)

	return err
}

// DeleteClientInvoice - Eliminar factura de un cliente
func DeleteClientInvoice(ctx context.Context, clienteID, invoiceID string) error {
	if Pool == nil {
		return ErrNoDatabase
	}

	query := `DELETE FROM facturas_clientes WHERE cliente_id = $1::uuid AND id = $2::uuid`
	_, err := Pool.Exec(ctx, query, clienteID, invoiceID)
	return err
}
