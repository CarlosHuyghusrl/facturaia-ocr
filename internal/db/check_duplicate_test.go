package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMain is defined in client_invoices_test.go — shared for this package.
//
// Huyghu test constants:
//   testClienteID  = "214538f1-536d-4c6e-a0a8-4d50d02070fb"
//   huyghuEmpresaID = "616b8f1b-d3f1-403d-883b-aec3302363e5"
//
// DB triggers note:
//   trg_auto_tag_606/607 BEFORE INSERT/UPDATE override aplica_606/aplica_607
//   based on NCF prefix in dgii_ncf_tipos + required fields gate.
//   B01 prefix:  aplica_606=true + aplica_607=true (if emisor_rnc non-empty + ncf>=11 + fecha + monto>0)
//   E31 prefix:  aplica_606=true + aplica_607=true (same conditions)
//   B02/E32 prefix: aplica_606=false, aplica_607=true (consumidor final / venta)
//
// To test aplica_606-only we INSERT with B01 then UPDATE aplica_607=false (bypassing trigger
// which only fires on UPDATE if we reset the flags explicitly after, but simplest is to
// use direct UPDATE after insert to force the scenario).

const (
	testClienteID = "214538f1-536d-4c6e-a0a8-4d50d02070fb"
	// Fake UUID for cross-client isolation tests (does not exist in clientes table)
	otherClienteID = "aaaaaaaa-0000-0000-0000-000000000001"
)

// uniqueNCF generates a test NCF string with B01 prefix (known to trigger aplica_606=true).
func uniqueNCF(suffix string) string {
	return fmt.Sprintf("B01%08s", suffix) // B01 + 8+ chars = 11+ total
}

// insertTestFactura inserts a row and optionally force-sets aplica_606/aplica_607
// via a follow-up UPDATE (triggers fire on INSERT but not if we UPDATE after).
// Returns the inserted row ID. Caller must DELETE in defer.
func insertTestFactura(t *testing.T, ctx context.Context, clienteID, ncf string) string {
	t.Helper()
	var id string
	err := Pool.QueryRow(ctx, `
		INSERT INTO facturas_clientes (
			cliente_id, empresa_id, ncf, monto, estado,
			fecha_documento, archivo_url, archivo_nombre,
			emisor_rnc
		)
		SELECT $1::uuid,
		       (SELECT empresa_id FROM clientes WHERE id = $1::uuid LIMIT 1),
		       $2, 5000.00, 'procesado',
		       $3, 'test://check-dup-test.jpg', 'check-dup-test.jpg',
		       '101000001'
		RETURNING id::text
	`, clienteID, ncf, time.Now().Format("2006-01-02")).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestFactura INSERT failed: %v", err)
	}
	return id
}

// forceAplicaFlags does a direct UPDATE bypassing trigger to force specific aplica flags.
// Used for testing DistintoTipo edge case.
func forceAplicaFlags(t *testing.T, ctx context.Context, id string, aplica606, aplica607 bool) {
	t.Helper()
	_, err := Pool.Exec(ctx,
		`UPDATE facturas_clientes SET aplica_606 = $2, aplica_607 = $3 WHERE id = $1::uuid`,
		id, aplica606, aplica607)
	if err != nil {
		t.Fatalf("forceAplicaFlags UPDATE failed: %v", err)
	}
}

// TestCheckDuplicateNCF_NoExists — NCF no existe → exists=false
func TestCheckDuplicateNCF_NoExists(t *testing.T) {
	ctx := context.Background()
	ncf := uniqueNCF("NOEX0001")

	result, err := CheckDuplicateNCFByTipo(ctx, testClienteID, ncf, "606")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Exists {
		t.Errorf("expected exists=false for NCF %s (not inserted), got exists=true", ncf)
	}
}

// TestCheckDuplicateNCF_Exists — NCF existente aplica_606=true → exists=true + datos retornados
func TestCheckDuplicateNCF_Exists(t *testing.T) {
	ctx := context.Background()
	ncf := uniqueNCF("EXST0002")

	id := insertTestFactura(t, ctx, testClienteID, ncf)
	defer func() {
		Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, id)
	}()

	// Trigger should have set aplica_606=true (B01 prefix + emisor_rnc set)
	result, err := CheckDuplicateNCFByTipo(ctx, testClienteID, ncf, "606")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Exists {
		t.Errorf("expected exists=true for NCF %s (B01 prefix, emisor_rnc set), got exists=false", ncf)
	}
	if result.ID != id {
		t.Errorf("expected ID=%s, got ID=%s", id, result.ID)
	}
	if result.NCF != ncf {
		t.Errorf("expected NCF=%s, got NCF=%s", ncf, result.NCF)
	}
}

// TestCheckDuplicateNCF_DistintoTipo — NCF aplica_606=true only, queried as tipo=607 → exists=false.
// Uses forceAplicaFlags to set aplica_606=true / aplica_607=false after INSERT.
func TestCheckDuplicateNCF_DistintoTipo(t *testing.T) {
	ctx := context.Background()
	ncf := uniqueNCF("TIPO0003")

	id := insertTestFactura(t, ctx, testClienteID, ncf)
	defer func() {
		Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, id)
	}()

	// Force aplica_606=true, aplica_607=false (to test tipo=607 returns false)
	forceAplicaFlags(t, ctx, id, true, false)

	result, err := CheckDuplicateNCFByTipo(ctx, testClienteID, ncf, "607")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Exists {
		t.Errorf("expected exists=false (row aplica_606=true/aplica_607=false, querying tipo=607), got exists=true")
	}
}

// TestCheckDuplicateNCF_NCFVacio — NCF empty → exists=false siempre (UNIQUE PARCIAL migration 20260501)
func TestCheckDuplicateNCF_NCFVacio(t *testing.T) {
	ctx := context.Background()

	result, err := CheckDuplicateNCFByTipo(ctx, testClienteID, "", "606")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Exists {
		t.Errorf("expected exists=false for empty NCF, got exists=true")
	}
}

// TestCheckDuplicateNCF_eCF — e-CF formato E31XXXXXXXX aplica_607=true → exists=true cuando tipo=607.
// Note: trg_auto_tag_607 only sets aplica_607=true when emisor_rnc = cliente's own RNC (venta perspective).
// For purchase e-CF (aplica_607=true forced manually), we use forceAplicaFlags after INSERT.
func TestCheckDuplicateNCF_eCF(t *testing.T) {
	ctx := context.Background()
	ncf := "E31TESTECF00050" // e-CF NCF format (E31 prefix, 15 chars)

	id := insertTestFactura(t, ctx, testClienteID, ncf)
	defer func() {
		Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, id)
	}()

	// Force aplica_607=true (trigger won't set it for purchase perspective)
	forceAplicaFlags(t, ctx, id, false, true)

	result, err := CheckDuplicateNCFByTipo(ctx, testClienteID, ncf, "607")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Exists {
		t.Errorf("expected exists=true for e-CF NCF %s (aplica_607=true forced), got exists=false", ncf)
	}
}

// TestCheckDuplicateNCF_MultiCliente — same NCF, distinto cliente → exists=false
// Verifica que cliente_id aisle correctamente (no cross-client data leak).
func TestCheckDuplicateNCF_MultiCliente(t *testing.T) {
	ctx := context.Background()
	ncf := uniqueNCF("MCLT0006")

	id := insertTestFactura(t, ctx, testClienteID, ncf)
	defer func() {
		Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, id)
	}()

	// Query with a different (non-existent) cliente → should return false
	result, err := CheckDuplicateNCFByTipo(ctx, otherClienteID, ncf, "606")
	// err acceptable if otherClienteID has no empresa (subquery returns null → empresa_id=null → never matches)
	if err != nil {
		t.Logf("non-critical error for unknown clienteID (expected for non-existent client): %v", err)
	}
	if result != nil && result.Exists {
		t.Errorf("expected exists=false for different clienteID, got exists=true (cross-client data leak!)")
	}
}

// TestCheckDuplicateNCF_MultiTenant — empresa_id viene del JOIN (no del caller).
// Verifica que la factura encontrada pertenece a la empresa correcta via JOIN.
func TestCheckDuplicateNCF_MultiTenant(t *testing.T) {
	ctx := context.Background()
	ncf := uniqueNCF("MTNT0007")

	id := insertTestFactura(t, ctx, testClienteID, ncf)
	defer func() {
		Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, id)
	}()

	// Querying con mismo clienteID → empresa_id matchea via JOIN → debe encontrar la factura
	result, err := CheckDuplicateNCFByTipo(ctx, testClienteID, ncf, "606")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Exists {
		t.Errorf("expected exists=true (same cliente+empresa via JOIN), got exists=false")
	}
	if result.ID != id {
		t.Errorf("expected ID=%s, got %s", id, result.ID)
	}
}
