package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// DuplicateCheckResult holds the result of a duplicate NCF check.
type DuplicateCheckResult struct {
	Exists   bool
	ID       string
	NCF      string
	Fecha    string
	Monto    float64
}

// CheckDuplicateNCFByTipo checks if a factura with the same NCF already exists
// for the same cliente, matching the given tipo (606 or 607).
//
// Multi-tenant safety: empresa_id is derived from the clientes table via clienteID
// (never trusted from query params).
//
// Edge cases handled:
//   - NCF empty: always returns Exists=false (allowed by UNIQUE PARCIAL migration 20260501)
//   - tipo "607": checks aplica_607 = true
//   - tipo "606" (default): checks aplica_606 = true
//   - Different tipo for same NCF: allowed (e.g. B01 in both 606+607 edge case)
//   - Different cliente: excluded (cross-cliente OK even same empresa)
func CheckDuplicateNCFByTipo(ctx context.Context, clienteID, ncf, tipo string) (*DuplicateCheckResult, error) {
	// NCF vacío → siempre permitir (UNIQUE PARCIAL migration 20260501 ya cubre)
	if ncf == "" {
		return &DuplicateCheckResult{Exists: false}, nil
	}

	if Pool == nil {
		return &DuplicateCheckResult{Exists: false}, ErrNoDatabase
	}

	// Column to check based on tipo
	aplicaCol := "aplica_606"
	if tipo == "607" {
		aplicaCol = "aplica_607"
	}

	// Query: same cliente_id + same empresa_id (via clientes join for multi-tenant safety) + same NCF + same tipo.
	// empresa_id derived from clientes.empresa_id, NOT from caller — prevents cross-tenant injection.
	query := `
		SELECT fc.id, fc.ncf, COALESCE(fc.fecha_documento::text, ''), COALESCE(fc.monto, 0)
		FROM facturas_clientes fc
		WHERE fc.cliente_id = $1::uuid
		  AND fc.empresa_id = (SELECT empresa_id FROM clientes WHERE id = $1::uuid LIMIT 1)
		  AND fc.ncf = $2
		  AND fc.ncf IS NOT NULL AND fc.ncf <> ''
		  AND fc.` + aplicaCol + ` = true
		LIMIT 1
	`

	var result DuplicateCheckResult
	err := Pool.QueryRow(ctx, query, clienteID, ncf).Scan(
		&result.ID,
		&result.NCF,
		&result.Fecha,
		&result.Monto,
	)

	if err == pgx.ErrNoRows {
		return &DuplicateCheckResult{Exists: false}, nil
	}
	if err != nil {
		return &DuplicateCheckResult{Exists: false}, err
	}

	result.Exists = true
	return &result, nil
}
