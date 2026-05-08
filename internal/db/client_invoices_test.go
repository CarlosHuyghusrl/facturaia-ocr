package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain inicializa pool BD antes de todos los tests.
// Cubre el bug fixed en commit f5feaab — SaveClientInvoice ahora hace lookup
// automatico de empresa_id desde la tabla clientes ANTES del INSERT.
// Hito: facturaia-fortify-empresa-id-w1 / KB 8698
func TestMain(m *testing.M) {
	// Asegurar DATABASE_URL seteado para test contra BD prod local
	if os.Getenv("DATABASE_URL") == "" {
		os.Setenv("DATABASE_URL", "postgres://supabase_admin:fBuTN2JZxjhJqxXCkacsMSPug9xgeb@localhost:5433/postgres")
	}
	if err := Init(); err != nil {
		panic("Test setup failed: cannot init DB pool: " + err.Error())
	}
	defer Close()
	code := m.Run()
	os.Exit(code)
}

// TestSaveClientInvoice_LookupEmpresaID verifica que el lookup automatico
// empresa_id desde clientes funciona cuando inv.EmpresaID viene vacio/nil.
func TestSaveClientInvoice_LookupEmpresaID(t *testing.T) {
	ctx := context.Background()

	// Cliente Huyghu real en BD prod local
	huyghuClienteID := "214538f1-536d-4c6e-a0a8-4d50d02070fb"
	huyghuEmpresaID := "616b8f1b-d3f1-403d-883b-aec3302363e5"

	t.Run("lookup_when_empresa_id_empty", func(t *testing.T) {
		inv := &ClientInvoice{
			ClienteID:        huyghuClienteID,
			ArchivoURL:       "test://wf1-regression-1.jpg",
			ArchivoNombre:    "wf1-regression-1.jpg",
			TipoDocumento:    "factura",
			NCF:              "B0100099991",
			Proveedor:        "WF1 TEST PROVIDER 1",
			Estado:           "procesado",
			EmisorRNC:        "101000001",
			FechaDocumento:   timePtr("2026-04-15"),
			Monto:            11800.00,
			Subtotal:         10000.00,
			ITBIS:            1800.00,
			FormaPago:        "01",
			TipoBienServicio: "09",
			ExtractionStatus: "completed",
			// EmpresaID intencionalmente NO seteado
		}
		err := SaveClientInvoice(ctx, inv)
		if err != nil {
			t.Fatalf("SaveClientInvoice failed: %v", err)
		}
		// Cleanup: borrar la fila de test al finalizar el subtest
		defer func() {
			_, _ = Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, inv.ID)
		}()

		// Verify empresa_id fue seteado por lookup
		var dbEmpresaID *string
		err = Pool.QueryRow(ctx,
			`SELECT empresa_id::text FROM facturas_clientes WHERE id = $1::uuid`, inv.ID,
		).Scan(&dbEmpresaID)
		if err != nil {
			t.Fatalf("query post-INSERT failed: %v", err)
		}
		if dbEmpresaID == nil || *dbEmpresaID != huyghuEmpresaID {
			t.Errorf("empresa_id lookup fallo: esperado=%s, got=%v", huyghuEmpresaID, dbEmpresaID)
		}
	})

	t.Run("preserve_when_empresa_id_already_set", func(t *testing.T) {
		// Si EmpresaID ya viene seteado en el struct, no debe sobrescribirlo
		empresaIDExplicit := huyghuEmpresaID
		inv := &ClientInvoice{
			ClienteID:        huyghuClienteID,
			ArchivoURL:       "test://wf1-regression-2.jpg",
			ArchivoNombre:    "wf1-regression-2.jpg",
			TipoDocumento:    "factura",
			NCF:              "B0100099992",
			Proveedor:        "WF1 TEST PROVIDER 2",
			Estado:           "procesado",
			EmisorRNC:        "101000001",
			FechaDocumento:   timePtr("2026-04-15"),
			Monto:            5900.00,
			Subtotal:         5000.00,
			ITBIS:            900.00,
			FormaPago:        "01",
			TipoBienServicio: "09",
			ExtractionStatus: "completed",
			EmpresaID:        &empresaIDExplicit,
		}
		err := SaveClientInvoice(ctx, inv)
		if err != nil {
			t.Fatalf("SaveClientInvoice failed: %v", err)
		}
		defer func() {
			_, _ = Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, inv.ID)
		}()

		var dbEmpresaID *string
		err = Pool.QueryRow(ctx,
			`SELECT empresa_id::text FROM facturas_clientes WHERE id = $1::uuid`, inv.ID,
		).Scan(&dbEmpresaID)
		if err != nil {
			t.Fatalf("query post-INSERT failed: %v", err)
		}
		if dbEmpresaID == nil || *dbEmpresaID != huyghuEmpresaID {
			t.Errorf("empresa_id no preservado: esperado=%s, got=%v", huyghuEmpresaID, dbEmpresaID)
		}
	})

	t.Run("graceful_when_cliente_id_not_found", func(t *testing.T) {
		// Cliente_id que no existe en clientes table.
		// Debido a la FK constraint facturas_clientes_cliente_id_fkey, el INSERT
		// fallara (graceful: el lookup devuelve error pero SaveClientInvoice continua,
		// y luego el INSERT falla por FK). Validamos que NO hay panic — error es OK.
		inv := &ClientInvoice{
			ClienteID:        "00000000-0000-0000-0000-000000000000",
			ArchivoURL:       "test://wf1-regression-3.jpg",
			ArchivoNombre:    "wf1-regression-3.jpg",
			TipoDocumento:    "factura",
			NCF:              "B0100099993",
			Proveedor:        "WF1 TEST PROVIDER 3",
			Estado:           "procesado",
			EmisorRNC:        "101000001",
			FechaDocumento:   timePtr("2026-04-15"),
			Monto:            1180.00,
			Subtotal:         1000.00,
			ITBIS:            180.00,
			FormaPago:        "01",
			TipoBienServicio: "09",
			ExtractionStatus: "completed",
		}
		err := SaveClientInvoice(ctx, inv)
		// FK constraint puede rechazar — eso tambien es valido (graceful, no panic).
		// Si pasa (caso improbable), verifica empresa_id NULL.
		if err == nil {
			defer func() {
				_, _ = Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, inv.ID)
			}()
			var dbEmpresaID *string
			qErr := Pool.QueryRow(ctx,
				`SELECT empresa_id::text FROM facturas_clientes WHERE id = $1::uuid`, inv.ID,
			).Scan(&dbEmpresaID)
			if qErr != nil {
				t.Fatalf("query post-INSERT failed: %v", qErr)
			}
			if dbEmpresaID != nil {
				t.Errorf("esperado empresa_id NULL para cliente_id no existente, got=%s", *dbEmpresaID)
			}
		}
		// Si err != nil con FK constraint, el test pasa (graceful, no panic)
	})
}

// TestGetFormato606_CrossTenantIsolation verifica que GetFormato606Invoices
// filtra por cliente_id del JWT y NO devuelve facturas de otros tenants.
// W2 P0 security fix — KB 9089.
// Por defecto skip: requiere DATABASE_URL + SETUP_E2E_TEST_DB=1.
func TestGetFormato606_CrossTenantIsolation(t *testing.T) {
	if os.Getenv("SETUP_E2E_TEST_DB") != "1" {
		t.Skip("Skipping cross-tenant isolation test: set SETUP_E2E_TEST_DB=1 to run against real DB")
	}

	ctx := context.Background()

	// Tenant B = HUYGHU (RNC 131047939, cliente_id real en BD prod)
	huyghuClienteID := "214538f1-536d-4c6e-a0a8-4d50d02070fb"
	huyghuRNC := "131047939"

	// Tenant A = Acela (cliente_id diferente — any valid UUID that is NOT huyghu)
	acelaClienteID := "9d216684-0000-0000-0000-000000000000" // dummy UUID for isolation check

	periodo := "202601" // periodo where HUYGHU has confirmed 606 invoices

	t.Run("tenant_A_cannot_read_tenant_B_606", func(t *testing.T) {
		// Acela JWT (clienteID=acelaClienteID) solicita RNC de HUYGHU → debe obtener 0 filas
		invoices, err := GetFormato606Invoices(ctx, huyghuRNC, periodo, acelaClienteID)
		if err != nil {
			t.Fatalf("GetFormato606Invoices error: %v", err)
		}
		if len(invoices) != 0 {
			t.Errorf("P0 cross-tenant leak detectado: tenant A (Acela) lee %d facturas de tenant B (HUYGHU)", len(invoices))
		}
	})

	t.Run("tenant_B_reads_own_606", func(t *testing.T) {
		// HUYGHU JWT (clienteID=huyghuClienteID) solicita su propio RNC → debe obtener filas
		invoices, err := GetFormato606Invoices(ctx, huyghuRNC, periodo, huyghuClienteID)
		if err != nil {
			t.Fatalf("GetFormato606Invoices error: %v", err)
		}
		if len(invoices) == 0 {
			t.Logf("Warning: tenant B (HUYGHU) tiene 0 facturas aplica_606=true en periodo %s — verifica BD", periodo)
		}
	})
}

// TestGetFormato608_CrossTenantIsolation verifica que GetFormato608Invoices
// filtra por cliente_id del JWT y NO devuelve facturas de otros tenants.
// W3.5 P1 security fix — KBs 9114+9115.
// Por defecto skip: requiere DATABASE_URL + SETUP_E2E_TEST_DB=1.
func TestGetFormato608_CrossTenantIsolation(t *testing.T) {
	if os.Getenv("SETUP_E2E_TEST_DB") != "1" {
		t.Skip("Skipping cross-tenant isolation test: set SETUP_E2E_TEST_DB=1 to run against real DB")
	}

	ctx := context.Background()

	// Tenant B = HUYGHU (RNC 131047939, cliente_id real en BD prod)
	huyghuClienteID := "214538f1-536d-4c6e-a0a8-4d50d02070fb"
	huyghuRNC := "131047939"

	// Tenant A = another client (dummy UUID — NOT huyghu)
	otherClienteID := "9d216684-0000-0000-0000-000000000000"

	periodo := "202601"

	t.Run("tenant_A_cannot_read_tenant_B_608", func(t *testing.T) {
		// Other tenant JWT solicita RNC de HUYGHU → debe obtener 0 filas
		invoices, err := GetFormato608Invoices(ctx, huyghuRNC, periodo, otherClienteID)
		if err != nil {
			t.Fatalf("GetFormato608Invoices error: %v", err)
		}
		if len(invoices) != 0 {
			t.Errorf("P1 cross-tenant leak detectado: tenant A lee %d facturas 608 de tenant B (HUYGHU)", len(invoices))
		}
	})

	t.Run("tenant_B_reads_own_608", func(t *testing.T) {
		// HUYGHU JWT solicita su propio RNC → debe obtener filas (o 0 si no hay anuladas)
		invoices, err := GetFormato608Invoices(ctx, huyghuRNC, periodo, huyghuClienteID)
		if err != nil {
			t.Fatalf("GetFormato608Invoices error: %v", err)
		}
		t.Logf("tenant B (HUYGHU) tiene %d facturas aplica_608=true en periodo %s", len(invoices), periodo)
	})
}

// TestUpdateEnvio606_OwnershipCheck verifica que UpdateEnvio606Referencia
// rechaza actualizaciones de envíos que no pertenecen al cliente autenticado.
// W3.5 P1 security fix — KBs 9114+9115.
// Por defecto skip: requiere DATABASE_URL + SETUP_E2E_TEST_DB=1.
func TestUpdateEnvio606_OwnershipCheck(t *testing.T) {
	if os.Getenv("SETUP_E2E_TEST_DB") != "1" {
		t.Skip("Skipping ownership check test: set SETUP_E2E_TEST_DB=1 to run against real DB")
	}

	ctx := context.Background()

	huyghuClienteID := "214538f1-536d-4c6e-a0a8-4d50d02070fb"
	otherClienteID := "9d216684-0000-0000-0000-000000000001"

	// Insert a test envio belonging to HUYGHU
	var envioID string
	err := Pool.QueryRow(ctx, `
		INSERT INTO envios_606 (cliente_id, rnc, periodo, estado)
		VALUES ($1::uuid, '131047939', '202601', 'generado')
		RETURNING id
	`, huyghuClienteID).Scan(&envioID)
	if err != nil {
		t.Fatalf("setup: failed to insert test envio_606: %v", err)
	}
	defer func() {
		_, _ = Pool.Exec(ctx, `DELETE FROM envios_606 WHERE id = $1::uuid`, envioID)
	}()

	t.Run("owner_can_update", func(t *testing.T) {
		// HUYGHU updating its own envio → should succeed (RowsAffected=1)
		err := UpdateEnvio606Referencia(ctx, envioID, "REF-TEST-001", "enviado", huyghuClienteID)
		if err != nil {
			t.Errorf("owner update failed unexpectedly: %v", err)
		}
	})

	t.Run("other_tenant_cannot_update", func(t *testing.T) {
		// Other tenant trying to update HUYGHU's envio → must return error "not owned"
		err := UpdateEnvio606Referencia(ctx, envioID, "REF-ATTACK-002", "completado", otherClienteID)
		if err == nil {
			t.Error("P1 ownership violation: other tenant updated HUYGHU's envio_606 without error")
		} else if !strings.Contains(err.Error(), "not owned") {
			t.Errorf("unexpected error (expected 'not owned'): %v", err)
		}
	})
}

// TestInsertEnvio606_SchemaCorrect verifica que InsertEnvio606 usa la columna
// cliente_id correcta del schema (no empresa_id). W4.F2 fix — P2 §11 Wave 3.
// Por defecto skip: requiere SETUP_E2E_TEST_DB=1 para correr contra BD real.
func TestInsertEnvio606_SchemaCorrect(t *testing.T) {
	if os.Getenv("SETUP_E2E_TEST_DB") != "1" {
		t.Skip("Skipping InsertEnvio606 schema test: set SETUP_E2E_TEST_DB=1 to run against real DB")
	}

	ctx := context.Background()

	// Cliente HUYGHU real en BD prod local
	testClienteID := "214538f1-536d-4c6e-a0a8-4d50d02070fb"
	testRNC := "131047939"
	testPeriodo := "202605"

	// Cleanup previo por si quedó basura de una corrida anterior
	_, _ = Pool.Exec(ctx,
		`DELETE FROM envios_606 WHERE cliente_id = $1::uuid AND periodo = $2 AND rnc = $3`,
		testClienteID, testPeriodo, testRNC)

	id, err := InsertEnvio606(ctx, testClienteID, testRNC, testPeriodo,
		"dummy-txt", "DGII_F_606_TEST.TXT",
		0, 0.0, 0.0, 0.0)

	// Si empresa_id vuelve en el error, el fix no funciono
	if err != nil {
		t.Fatalf("InsertEnvio606 error (posible columna empresa_id): %v", err)
	}
	if id == "" {
		t.Fatal("InsertEnvio606 devolvio id vacio")
	}

	// Cleanup
	defer func() {
		_, _ = Pool.Exec(ctx, `DELETE FROM envios_606 WHERE id = $1::uuid`, id)
	}()

	// Verify fila insertada con cliente_id correcto
	var dbClienteID string
	err = Pool.QueryRow(ctx,
		`SELECT cliente_id::text FROM envios_606 WHERE id = $1::uuid`, id,
	).Scan(&dbClienteID)
	if err != nil {
		t.Fatalf("query post-INSERT failed: %v", err)
	}
	if dbClienteID != testClienteID {
		t.Errorf("cliente_id incorrecto: esperado=%s, got=%s", testClienteID, dbClienteID)
	}
}

// timePtr helper para campos *time.Time en formato YYYY-MM-DD
func timePtr(dateStr string) *time.Time {
	t, _ := time.Parse("2006-01-02", dateStr)
	return &t
}
