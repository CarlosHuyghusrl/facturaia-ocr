package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Invoice struct {
	ID              uuid.UUID  `json:"id"`
	NCF             string     `json:"ncf"`
	RNCProveedor    string     `json:"rnc_proveedor"`
	NombreProveedor string     `json:"nombre_proveedor"`
	FechaFactura    *time.Time `json:"fecha_factura"`
	Subtotal        float64    `json:"subtotal"`
	ITBIS           float64    `json:"itbis"`
	Total           float64    `json:"total"`
	TipoGasto       string     `json:"tipo_gasto"`
	ImagenURL       string     `json:"imagen_url"`
	OCRRaw          string     `json:"ocr_raw"`
	OCRJSON         string     `json:"ocr_json"`
	Estado          string     `json:"estado"`
	UsuarioID       uuid.UUID  `json:"usuario_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

func SaveInvoice(ctx context.Context, empresaAlias string, inv *Invoice) error {
	schema := GetSchemaForEmpresa(empresaAlias)

	query := fmt.Sprintf(`
		INSERT INTO %s.facturas (
			ncf, rnc_proveedor, nombre_proveedor, fecha_factura,
			subtotal, itbis, total, tipo_gasto, imagen_url,
			ocr_raw, ocr_json, estado, usuario_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at
	`, schema)

	err := Pool.QueryRow(ctx, query,
		inv.NCF, inv.RNCProveedor, inv.NombreProveedor, inv.FechaFactura,
		inv.Subtotal, inv.ITBIS, inv.Total, inv.TipoGasto, inv.ImagenURL,
		inv.OCRRaw, inv.OCRJSON, inv.Estado, inv.UsuarioID,
	).Scan(&inv.ID, &inv.CreatedAt)

	return err
}

func GetInvoices(ctx context.Context, empresaAlias string, limit int) ([]Invoice, error) {
	schema := GetSchemaForEmpresa(empresaAlias)

	query := fmt.Sprintf(`
		SELECT id, COALESCE(ncf, ''), COALESCE(rnc_proveedor, ''), COALESCE(nombre_proveedor, ''),
		       fecha_factura, COALESCE(subtotal, 0), COALESCE(itbis, 0), COALESCE(total, 0),
		       COALESCE(tipo_gasto, ''), COALESCE(imagen_url, ''), COALESCE(estado, ''), created_at
		FROM %s.facturas
		ORDER BY created_at DESC
		LIMIT $1
	`, schema)

	rows, err := Pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		err := rows.Scan(
			&inv.ID, &inv.NCF, &inv.RNCProveedor, &inv.NombreProveedor,
			&inv.FechaFactura, &inv.Subtotal, &inv.ITBIS, &inv.Total,
			&inv.TipoGasto, &inv.ImagenURL, &inv.Estado, &inv.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}

	return invoices, nil
}

// GetInvoiceByID retrieves a single invoice by ID
func GetInvoiceByID(ctx context.Context, empresaAlias string, invoiceID string) (*Invoice, error) {
	schema := GetSchemaForEmpresa(empresaAlias)

	query := fmt.Sprintf(`
		SELECT id, COALESCE(ncf, ''), COALESCE(rnc_proveedor, ''), COALESCE(nombre_proveedor, ''),
		       fecha_factura, COALESCE(subtotal, 0), COALESCE(itbis, 0), COALESCE(total, 0),
		       COALESCE(tipo_gasto, ''), COALESCE(imagen_url, ''), COALESCE(ocr_raw, ''), COALESCE(ocr_json::text, ''),
		       COALESCE(estado, ''), COALESCE(usuario_id, '00000000-0000-0000-0000-000000000000'::uuid), created_at, updated_at
		FROM %s.facturas
		WHERE id = $1
	`, schema)

	var inv Invoice
	err := Pool.QueryRow(ctx, query, invoiceID).Scan(
		&inv.ID, &inv.NCF, &inv.RNCProveedor, &inv.NombreProveedor,
		&inv.FechaFactura, &inv.Subtotal, &inv.ITBIS, &inv.Total,
		&inv.TipoGasto, &inv.ImagenURL, &inv.OCRRaw, &inv.OCRJSON,
		&inv.Estado, &inv.UsuarioID, &inv.CreatedAt, &inv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// UpdateInvoice updates invoice fields
func UpdateInvoice(ctx context.Context, empresaAlias string, invoiceID string, updates map[string]interface{}) error {
	schema := GetSchemaForEmpresa(empresaAlias)

	// Build dynamic UPDATE query
	sets := []string{}
	args := []interface{}{}
	i := 1
	for key, value := range updates {
		sets = append(sets, fmt.Sprintf("%s = $%d", key, i))
		args = append(args, value)
		i++
	}

	// Add updated_at
	sets = append(sets, fmt.Sprintf("updated_at = $%d", i))
	args = append(args, time.Now())
	i++

	// Add invoice ID as last parameter
	args = append(args, invoiceID)

	query := fmt.Sprintf("UPDATE %s.facturas SET %s WHERE id = $%d",
		schema, strings.Join(sets, ", "), i)

	_, err := Pool.Exec(ctx, query, args...)
	return err
}

// DeleteInvoice removes an invoice
func DeleteInvoice(ctx context.Context, empresaAlias string, invoiceID string) error {
	schema := GetSchemaForEmpresa(empresaAlias)
	query := fmt.Sprintf("DELETE FROM %s.facturas WHERE id = $1", schema)
	_, err := Pool.Exec(ctx, query, invoiceID)
	return err
}

// MonthlyStats represents monthly statistics
type MonthlyStats struct {
	Month          string  `json:"month"`
	TotalFacturas  int     `json:"total_facturas"`
	TotalSubtotal  float64 `json:"total_subtotal"`
	TotalITBIS     float64 `json:"total_itbis"`
	TotalMonto     float64 `json:"total_monto"`
}

// GetMonthlyStats returns statistics for current month
func GetMonthlyStats(ctx context.Context, empresaAlias string) (*MonthlyStats, error) {
	schema := GetSchemaForEmpresa(empresaAlias)

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_facturas,
			COALESCE(SUM(subtotal), 0) as total_subtotal,
			COALESCE(SUM(itbis), 0) as total_itbis,
			COALESCE(SUM(total), 0) as total_monto
		FROM %s.facturas
		WHERE DATE_TRUNC('month', created_at) = DATE_TRUNC('month', CURRENT_DATE)
	`, schema)

	stats := &MonthlyStats{
		Month: time.Now().Format("2006-01"),
	}

	err := Pool.QueryRow(ctx, query).Scan(
		&stats.TotalFacturas,
		&stats.TotalSubtotal,
		&stats.TotalITBIS,
		&stats.TotalMonto,
	)
	if err != nil {
		return nil, err
	}

	return stats, nil
}
