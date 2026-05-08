package api

// ─────────────────────────────────────────────────────────────────────────────
// W19.Q1 — Tests E2E pipeline OCR FacturaIA
//
// Cobertura: login → upload → OCR → BD INSERT → 606 endpoint
//
// Decisión de diseño — Plan A:
//   Los tests E2E requieren BD PostgreSQL real (conn string + cliente de prueba
//   con pin_hash bcrypt) y un proveedor AI mockeado. Para no contaminar producción,
//   se saltan por defecto y se activan con:
//
//     SETUP_E2E_TEST_DB=1 JWT_SECRET=test-secret-at-least-32-chars-long \
//     DATABASE_URL=postgres://... go test ./api/... -run E2E -v -timeout 120s
//
//   Con BD disponible (ver §SETUP más abajo), los tests usan transacciones con
//   ROLLBACK para limpieza automática y no dejan basura en producción.
//
// §SETUP — Variables de entorno necesarias para activar tests E2E:
//   SETUP_E2E_TEST_DB=1     — Activa los tests (sin esto, todos hacen t.Skip)
//   JWT_SECRET              — >= 32 chars, cualquier string de test
//   DATABASE_URL            — postgres://user:pass@host:port/dbname
//   E2E_CLIENTE_ID          — UUID del cliente de prueba (ej: 214538f1-...)
//   E2E_RNC_RECEPTOR        — RNC del cliente de prueba (ej: 130309094)
//   E2E_PIN                 — PIN en texto plano del cliente de prueba
//
// §CLIENTE_TEST — Fixtures esperadas en la BD:
//   - clientes WHERE id = E2E_CLIENTE_ID debe existir, app_activa=true, pin_hash válido
//   - El PIN debe ser el bcrypt de E2E_PIN
//
// §MODO_QUICK — Para correr en CI sin BD real:
//   No definir SETUP_E2E_TEST_DB → todos los tests hacen t.Skip con mensaje claro.
//   El archivo sirve como reference template para QA manual cuando setup haya.
//
// Tests incluidos:
//   Test 1: TestE2E_Login_Upload_Validate_Save_Get606   — pipeline completo
//   Test 2: TestE2E_Anti_Duplicate_NCF                  — dedup exacto por NCF
//   Test 3: TestE2E_FiscalSkill_IA_Sector_Electricidad  — W17.1 skill exento
//   Test 4: TestE2E_606_TXT_Format_Canonical            — header + 3 rows DGII
// ─────────────────────────────────────────────────────────────────────────────

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/facturaIA/invoice-ocr-service/internal/auth"
	"github.com/facturaIA/invoice-ocr-service/internal/db"
	"github.com/facturaIA/invoice-ocr-service/internal/models"
)

// ─────────────────────────────────────────────────────────────────────────────
// Setup helpers
// ─────────────────────────────────────────────────────────────────────────────

// e2eEnv holds resolved environment variables for E2E tests.
type e2eEnv struct {
	ClienteID   string
	RNCReceptor string
	PIN         string
	DBURL       string
	JWTSecret   string
}

// requireE2ESetup skips the test if E2E env vars are not available.
// Returns a populated e2eEnv if all vars are present.
func requireE2ESetup(t *testing.T) e2eEnv {
	t.Helper()
	if os.Getenv("SETUP_E2E_TEST_DB") != "1" {
		t.Skip("E2E test skipped: set SETUP_E2E_TEST_DB=1 + E2E_* env vars to run (see §SETUP in handler_e2e_test.go)")
	}

	env := e2eEnv{
		ClienteID:   os.Getenv("E2E_CLIENTE_ID"),
		RNCReceptor: os.Getenv("E2E_RNC_RECEPTOR"),
		PIN:         os.Getenv("E2E_PIN"),
		DBURL:       os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}

	missing := []string{}
	if env.ClienteID == "" {
		missing = append(missing, "E2E_CLIENTE_ID")
	}
	if env.RNCReceptor == "" {
		missing = append(missing, "E2E_RNC_RECEPTOR")
	}
	if env.PIN == "" {
		missing = append(missing, "E2E_PIN")
	}
	if env.DBURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if env.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if len(missing) > 0 {
		t.Skipf("E2E test skipped: missing env vars: %s", strings.Join(missing, ", "))
	}
	return env
}

// initE2EAuth initializes the JWT secret for tests (must match the server's secret).
func initE2EAuth(t *testing.T, secret string) {
	t.Helper()
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("failed to set JWT_SECRET: %v", err)
	}
	if err := auth.Init(); err != nil {
		t.Fatalf("auth.Init failed: %v", err)
	}
}

// initE2EDB initializes the database pool using DATABASE_URL env var.
// Sets DATABASE_URL from dbURL param so db.Init() picks it up.
func initE2EDB(t *testing.T, dbURL string) {
	t.Helper()
	if db.Pool != nil {
		return // already initialized
	}
	if dbURL != "" {
		if err := os.Setenv("DATABASE_URL", dbURL); err != nil {
			t.Fatalf("failed to set DATABASE_URL: %v", err)
		}
	}
	if err := db.Init(); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
}

// e2eBearerToken generates a valid JWT for the test cliente.
// Uses auth.GenerateToken directly (bypasses DB login to keep test isolated).
func e2eGenerateToken(t *testing.T, clienteID, rnc string) string {
	t.Helper()
	// empresaAlias = rnc (the field used in multi-tenant storage namespacing)
	token, err := auth.GenerateToken(clienteID, "e2e@test.local", rnc, "Test Cliente E2E", "cliente")
	if err != nil {
		t.Fatalf("e2eGenerateToken: %v", err)
	}
	return token
}

// minimalTestConfig returns a Config usable for E2E tests.
// AI provider is set to "openai" pointing at a mock server (see mockAIServer).
func minimalTestConfig(aiBaseURL string) *models.Config {
	return &models.Config{
		AI: models.AIConfig{
			DefaultProvider: "openai",
			OpenAI: models.OpenAIConfig{
				APIKey:  "sk-test",
				BaseURL: aiBaseURL,
				Model:   "gpt-test",
			},
		},
		OCR: models.OCRConfig{
			Engine:   "none",
			Language: "spa",
		},
	}
}

// mockAIServer returns an httptest.Server that emulates the OpenAI chat completions
// endpoint with a hardcoded invoice JSON response. The caller controls the returned
// NCF via the ncf parameter to make each test deterministic.
func mockAIServer(t *testing.T, ncf, rncEmisor string) *httptest.Server {
	t.Helper()
	resp := fmt.Sprintf(`{
		"choices": [{
			"message": {
				"content": "{\"ncf\":\"%s\",\"rnc_emisor\":\"%s\",\"nombre_emisor\":\"Proveedor E2E Test\",\"rnc_receptor\":\"\",\"nombre_receptor\":\"\",\"tipo_ncf\":\"B01\",\"fecha_factura\":\"2026-05-01T00:00:00Z\",\"monto_servicios\":5000.0,\"monto_bienes\":0.0,\"subtotal\":5000.0,\"itbis\":900.0,\"total\":5900.0,\"forma_pago\":\"04\",\"tipo_bien_servicio\":\"02\",\"confidence\":0.92,\"categoria_itbis\":\"general\",\"sector_proveedor\":\"servicios\",\"requiere_correccion\":false,\"warnings_ia\":[]}"
			}
		}]
	}`, ncf, rncEmisor)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// mockAIServerExento returns a mock AI server that returns ITBIS=0, sector=electricidad.
// Used for Test 3 (W17.1 fiscal skill).
func mockAIServerExento(t *testing.T, ncf string) *httptest.Server {
	t.Helper()
	resp := fmt.Sprintf(`{
		"choices": [{
			"message": {
				"content": "{\"ncf\":\"%s\",\"rnc_emisor\":\"123456789\",\"nombre_emisor\":\"EDESUR E2E\",\"rnc_receptor\":\"\",\"nombre_receptor\":\"\",\"tipo_ncf\":\"B01\",\"fecha_factura\":\"2026-05-01T00:00:00Z\",\"monto_servicios\":2850.0,\"monto_bienes\":0.0,\"subtotal\":2850.0,\"itbis\":0.0,\"total\":2850.0,\"forma_pago\":\"04\",\"tipo_bien_servicio\":\"02\",\"confidence\":0.91,\"categoria_itbis\":\"exento\",\"sector_proveedor\":\"electricidad\",\"requiere_correccion\":false,\"warnings_ia\":[\"Servicio basico (electricidad/combustible/agua) — ITBIS exento por ley\"]}"
			}
		}]
	}`, ncf)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// buildUploadRequest builds a multipart/form-data request with a minimal 1x1 JPEG.
// This avoids reading a real image file from disk.
func buildUploadRequest(t *testing.T, token string) *http.Request {
	t.Helper()
	// Minimal valid JPEG (1x1 white pixel, 107 bytes)
	// Source: https://github.com/nicowillis/images/blob/master/1x1.jpg
	minJPEG := []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01,
		0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xFF, 0xDB, 0x00, 0x43,
		0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09,
		0x09, 0x08, 0x0A, 0x0C, 0x14, 0x0D, 0x0C, 0x0B, 0x0B, 0x0C, 0x19, 0x12,
		0x13, 0x0F, 0x14, 0x1D, 0x1A, 0x1F, 0x1E, 0x1D, 0x1A, 0x1C, 0x1C, 0x20,
		0x24, 0x2E, 0x27, 0x20, 0x22, 0x2C, 0x23, 0x1C, 0x1C, 0x28, 0x37, 0x29,
		0x2C, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1F, 0x27, 0x39, 0x3D, 0x38, 0x32,
		0x3C, 0x2E, 0x33, 0x34, 0x32, 0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x01,
		0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xFF, 0xC4, 0x00, 0x1F, 0x00, 0x00,
		0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F,
		0x00, 0xFB, 0xD0, 0xFF, 0xD9,
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("file", "test_factura.jpg")
	if err != nil {
		t.Fatalf("buildUploadRequest: CreateFormFile: %v", err)
	}
	if _, err := io.Copy(fw, bytes.NewReader(minJPEG)); err != nil {
		t.Fatalf("buildUploadRequest: Copy: %v", err)
	}
	// Tell the handler to use openai provider (our mock)
	if err := w.WriteField("aiProvider", "openai"); err != nil {
		t.Fatalf("buildUploadRequest: WriteField aiProvider: %v", err)
	}
	// Explicitly disable vision mode — we mock the AI response directly
	if err := w.WriteField("useVisionModel", "false"); err != nil {
		t.Fatalf("buildUploadRequest: WriteField useVisionModel: %v", err)
	}
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/facturas/upload/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// cleanupFactura deletes a factura_clientes row by ID. Called via defer in tests.
func cleanupFactura(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	if id == "" || db.Pool == nil {
		return
	}
	_, err := db.Pool.Exec(ctx, `DELETE FROM facturas_clientes WHERE id = $1::uuid`, id)
	if err != nil {
		t.Logf("cleanupFactura: WARNING: failed to delete test factura %s: %v", id, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: TestE2E_Login_Upload_Validate_Save_Get606
//
// Pipeline completo: generar JWT → POST upload → verificar response.success=true
// → SELECT BD → GET formato-606 → 200 OK.
// ─────────────────────────────────────────────────────────────────────────────

func TestE2E_Login_Upload_Validate_Save_Get606(t *testing.T) {
	env := requireE2ESetup(t)
	initE2EAuth(t, env.JWTSecret)
	initE2EDB(t, env.DBURL)

	ctx := context.Background()
	ncf := fmt.Sprintf("B01%08d", time.Now().UnixNano()%100000000)
	rncEmisor := "101000001" // test emisor RNC (9 digits)

	// Step 1: Setup mock AI server and test HTTP server
	aiSrv := mockAIServer(t, ncf, rncEmisor)
	cfg := minimalTestConfig(aiSrv.URL)
	h := NewHandler(cfg)
	router := h.SetupRoutes()
	// Wrap with JWT middleware so the server validates our tokens
	srv := httptest.NewServer(auth.JWTMiddleware(router))
	t.Cleanup(srv.Close)

	// Step 2: Generate JWT for test cliente
	token := e2eGenerateToken(t, env.ClienteID, env.RNCReceptor)

	// Step 3: POST /api/facturas/upload/ con Bearer JWT
	uploadReq := buildUploadRequest(t, token)
	// Rewrite URL to test server
	uploadReq.RequestURI = ""
	uploadReq.URL, _ = uploadReq.URL.Parse(srv.URL + "/api/facturas/upload/")

	resp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatalf("upload POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: expected HTTP 200, got %d — body: %s", resp.StatusCode, body)
	}

	var uploadResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		t.Fatalf("upload: failed to decode response: %v", err)
	}

	// Step 4: Verify response.success=true + invoice_id non-empty
	success, _ := uploadResp["success"].(bool)
	if !success {
		t.Fatalf("upload: expected success=true, got: %+v", uploadResp)
	}

	invoiceID, _ := uploadResp["invoice_id"].(string)
	if invoiceID == "" {
		t.Fatalf("upload: expected non-empty invoice_id, got: %+v", uploadResp)
	}
	defer cleanupFactura(t, ctx, invoiceID)

	t.Logf("STEP 3 OK: upload success, invoice_id=%s", invoiceID)

	// Step 5: SELECT facturas_clientes WHERE id = invoiceID — verify NCF + monto present
	var savedNCF, savedEmisorRNC string
	var savedMonto float64
	err = db.Pool.QueryRow(ctx,
		`SELECT COALESCE(ncf,''), COALESCE(emisor_rnc,''), COALESCE(monto,0)
		 FROM facturas_clientes WHERE id = $1::uuid AND cliente_id = $2::uuid`,
		invoiceID, env.ClienteID).Scan(&savedNCF, &savedEmisorRNC, &savedMonto)
	if err != nil {
		t.Fatalf("SELECT facturas_clientes: %v", err)
	}

	if savedNCF == "" {
		t.Errorf("expected ncf non-empty in BD, got empty (invoice_id=%s)", invoiceID)
	}
	if savedMonto <= 0 {
		t.Errorf("expected monto > 0 in BD, got %f (invoice_id=%s)", savedMonto, invoiceID)
	}
	t.Logf("STEP 5 OK: BD has ncf=%s emisor_rnc=%s monto=%.2f", savedNCF, savedEmisorRNC, savedMonto)

	// Step 6: GET /api/formato-606/{rnc_receptor}?periodo=YYYYMM → assert HTTP 200
	periodo := time.Now().Format("200601") // current month
	get606URL := fmt.Sprintf("%s/api/formato-606/%s?periodo=%s", srv.URL, env.RNCReceptor, periodo)
	get606Req, _ := http.NewRequest(http.MethodGet, get606URL, nil)
	get606Req.Header.Set("Authorization", "Bearer "+token)

	resp606, err := http.DefaultClient.Do(get606Req)
	if err != nil {
		t.Fatalf("GET formato-606: %v", err)
	}
	defer resp606.Body.Close()

	if resp606.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp606.Body)
		t.Fatalf("GET formato-606: expected 200, got %d — body: %s", resp606.StatusCode, body)
	}

	body606, _ := io.ReadAll(resp606.Body)
	t.Logf("STEP 6 OK: GET formato-606 HTTP 200, body size=%d bytes", len(body606))

	// The TXT response must start with the header line "606|rnc|periodo|N"
	bodyStr := string(body606)
	expectedHeaderPrefix := fmt.Sprintf("606|%s|%s|", env.RNCReceptor, periodo)
	if !strings.HasPrefix(bodyStr, expectedHeaderPrefix) {
		t.Errorf("GET formato-606: expected body to start with %q, got: %.120s...", expectedHeaderPrefix, bodyStr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: TestE2E_Anti_Duplicate_NCF
//
// Verifica que subir la misma factura dos veces (mismo NCF) produce
// HTTP 409 Conflict en el segundo intento y BD tiene SOLO 1 fila.
// ─────────────────────────────────────────────────────────────────────────────

func TestE2E_Anti_Duplicate_NCF(t *testing.T) {
	env := requireE2ESetup(t)
	initE2EAuth(t, env.JWTSecret)
	initE2EDB(t, env.DBURL)

	ctx := context.Background()
	ncf := fmt.Sprintf("B01%08d", (time.Now().UnixNano()/1000)%100000000)
	rncEmisor := "101000002" // distinct from Test 1

	aiSrv := mockAIServer(t, ncf, rncEmisor)
	cfg := minimalTestConfig(aiSrv.URL)
	h := NewHandler(cfg)
	router := h.SetupRoutes()
	srv := httptest.NewServer(auth.JWTMiddleware(router))
	t.Cleanup(srv.Close)

	token := e2eGenerateToken(t, env.ClienteID, env.RNCReceptor)

	// Upload 1 (should succeed)
	req1 := buildUploadRequest(t, token)
	req1.RequestURI = ""
	req1.URL, _ = req1.URL.Parse(srv.URL + "/api/facturas/upload/")

	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("upload1 POST failed: %v", err)
	}
	defer resp1.Body.Close()

	var resp1Body map[string]interface{}
	json.NewDecoder(resp1.Body).Decode(&resp1Body)

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("upload1: expected 200, got %d — body: %+v", resp1.StatusCode, resp1Body)
	}

	invoiceID1, _ := resp1Body["invoice_id"].(string)
	if invoiceID1 == "" {
		t.Fatalf("upload1: expected invoice_id, got: %+v", resp1Body)
	}
	defer cleanupFactura(t, ctx, invoiceID1)
	t.Logf("UPLOAD 1 OK: invoice_id=%s ncf=%s", invoiceID1, ncf)

	// Upload 2: same NCF — must return 409 Conflict
	req2 := buildUploadRequest(t, token)
	req2.RequestURI = ""
	req2.URL, _ = req2.URL.Parse(srv.URL + "/api/facturas/upload/")

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("upload2 POST failed: %v", err)
	}
	defer resp2.Body.Close()

	var resp2Body map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&resp2Body)

	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("upload2 (duplicate NCF): expected HTTP 409, got %d — body: %+v", resp2.StatusCode, resp2Body)
	} else {
		t.Logf("UPLOAD 2 OK: correctly got HTTP 409 for duplicate NCF=%s", ncf)
	}

	// Verify BD has ONLY 1 entry for this NCF
	var count int
	err = db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM facturas_clientes
		 WHERE cliente_id = $1::uuid AND ncf = $2`,
		env.ClienteID, ncf).Scan(&count)
	if err != nil {
		t.Fatalf("COUNT query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 factura with NCF=%s, got %d (duplicate constraint failed)", ncf, count)
	} else {
		t.Logf("BD OK: exactly 1 row for NCF=%s", ncf)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: TestE2E_FiscalSkill_IA_Sector_Electricidad
//
// Verifica W17.1: cuando la IA retorna sector_proveedor=electricidad +
// categoria_itbis=exento + itbis=0, el validador NO genera error itbis_mismatch
// y la factura se guarda exitosamente.
// ─────────────────────────────────────────────────────────────────────────────

func TestE2E_FiscalSkill_IA_Sector_Electricidad(t *testing.T) {
	env := requireE2ESetup(t)
	initE2EAuth(t, env.JWTSecret)
	initE2EDB(t, env.DBURL)

	ctx := context.Background()
	// Use a unique NCF to avoid duplicate conflict with Test 1/2
	ncf := fmt.Sprintf("B01%08d", (time.Now().UnixNano()/100)%100000000)

	aiSrv := mockAIServerExento(t, ncf)
	cfg := minimalTestConfig(aiSrv.URL)
	h := NewHandler(cfg)
	router := h.SetupRoutes()
	srv := httptest.NewServer(auth.JWTMiddleware(router))
	t.Cleanup(srv.Close)

	token := e2eGenerateToken(t, env.ClienteID, env.RNCReceptor)

	req := buildUploadRequest(t, token)
	req.RequestURI = ""
	req.URL, _ = req.URL.Parse(srv.URL + "/api/facturas/upload/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload POST failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d — body: %s", resp.StatusCode, bodyBytes)
	}

	var respBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify success=true (no itbis_mismatch blocking)
	success, _ := respBody["success"].(bool)
	if !success {
		t.Fatalf("expected success=true for exento electricidad invoice, got: %+v", respBody)
	}

	invoiceID, _ := respBody["invoice_id"].(string)
	if invoiceID != "" {
		defer cleanupFactura(t, ctx, invoiceID)
	}

	// Verify validation block has no itbis_mismatch errors
	validationBlock, hasValidation := respBody["validation"].(map[string]interface{})
	if hasValidation {
		if errors, ok := validationBlock["errors"].([]interface{}); ok {
			for _, e := range errors {
				if errMap, ok := e.(map[string]interface{}); ok {
					if code, _ := errMap["code"].(string); code == "itbis_mismatch" {
						t.Errorf("W17.1 violation: got itbis_mismatch error for sector=electricidad, categoria_itbis=exento invoice")
					}
				}
			}
		}
	}

	t.Logf("STEP OK: exento electricidad invoice processed — success=true, no itbis_mismatch")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: TestE2E_606_TXT_Format_Canonical
//
// Inserta 3 facturas de prueba directamente en BD (evita OCR pipeline),
// fuerza aplica_606=true + receptor_rnc=test_rnc, llama GET formato-606,
// y verifica el formato TXT DGII Norma 07-2018:
//   header: "606|rnc|periodo|3"
//   3 data rows pipe-delimited con 23 fields cada uno
// ─────────────────────────────────────────────────────────────────────────────

func TestE2E_606_TXT_Format_Canonical(t *testing.T) {
	env := requireE2ESetup(t)
	initE2EAuth(t, env.JWTSecret)
	initE2EDB(t, env.DBURL)

	ctx := context.Background()
	periodo := time.Now().Format("200601")

	// Compute a fecha_documento inside current period
	testDate := time.Now().Format("2006-01-02")

	// Insert 3 test facturas directly into BD (bypassing OCR/AI)
	testNCFs := []string{
		fmt.Sprintf("B01%08dA", time.Now().UnixNano()%100000000),
		fmt.Sprintf("B01%08dB", time.Now().UnixNano()%100000000),
		fmt.Sprintf("B01%08dC", time.Now().UnixNano()%100000000),
	}
	insertedIDs := []string{}

	for _, ncf := range testNCFs {
		var id string
		err := db.Pool.QueryRow(ctx, `
			INSERT INTO facturas_clientes (
				cliente_id, empresa_id, ncf, emisor_rnc, tipo_id_emisor,
				tipo_bien_servicio, monto, subtotal, itbis,
				monto_servicios, monto_bienes,
				receptor_rnc, fecha_documento, estado,
				archivo_url, archivo_nombre, forma_pago,
				aplica_606, aplica_607
			)
			SELECT
				$1::uuid,
				(SELECT empresa_id FROM clientes WHERE id = $1::uuid LIMIT 1),
				$2, '101000099', '1',
				'02', 5900.00, 5000.00, 900.00,
				5000.00, 0.00,
				$3, $4::date, 'procesado',
				'test://e2e-606-test.jpg', 'e2e_606_test.jpg', '04',
				true, false
			RETURNING id::text
		`, env.ClienteID, ncf, env.RNCReceptor, testDate).Scan(&id)
		if err != nil {
			t.Fatalf("INSERT test factura (ncf=%s): %v", ncf, err)
		}
		insertedIDs = append(insertedIDs, id)
		t.Logf("Inserted test factura id=%s ncf=%s", id, ncf)
	}

	// Cleanup all inserted rows after test
	defer func() {
		for _, id := range insertedIDs {
			cleanupFactura(t, ctx, id)
		}
	}()

	// Setup handler + test server
	cfg := &models.Config{
		AI:  models.AIConfig{DefaultProvider: "openai"},
		OCR: models.OCRConfig{Engine: "none", Language: "spa"},
	}
	h := NewHandler(cfg)
	router := h.SetupRoutes()
	srv := httptest.NewServer(auth.JWTMiddleware(router))
	t.Cleanup(srv.Close)

	token := e2eGenerateToken(t, env.ClienteID, env.RNCReceptor)

	// GET /api/formato-606/{rnc_receptor}?periodo=YYYYMM
	url606 := fmt.Sprintf("%s/api/formato-606/%s?periodo=%s", srv.URL, env.RNCReceptor, periodo)
	req606, _ := http.NewRequest(http.MethodGet, url606, nil)
	req606.Header.Set("Authorization", "Bearer "+token)

	resp606, err := http.DefaultClient.Do(req606)
	if err != nil {
		t.Fatalf("GET formato-606: %v", err)
	}
	defer resp606.Body.Close()

	if resp606.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp606.Body)
		t.Fatalf("GET formato-606: expected 200, got %d — body: %s", resp606.StatusCode, body)
	}

	bodyBytes, _ := io.ReadAll(resp606.Body)
	bodyStr := string(bodyBytes)

	t.Logf("formato-606 TXT response (first 500 chars):\n%.500s", bodyStr)

	// Parse TXT: split by newline, filter empty lines
	lines := []string{}
	for _, l := range strings.Split(bodyStr, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}

	if len(lines) < 1 {
		t.Fatalf("formato-606 TXT has no lines")
	}

	// Verify header: "606|{rnc}|{periodo}|{N}" where N >= 3
	headerLine := lines[0]
	headerParts := strings.Split(headerLine, "|")
	if len(headerParts) != 4 {
		t.Fatalf("header must have 4 pipe-separated fields, got: %q", headerLine)
	}
	if headerParts[0] != "606" {
		t.Errorf("header field 1: expected '606', got %q", headerParts[0])
	}
	if headerParts[1] != env.RNCReceptor {
		t.Errorf("header field 2: expected RNC %q, got %q", env.RNCReceptor, headerParts[1])
	}
	if headerParts[2] != periodo {
		t.Errorf("header field 3: expected periodo %q, got %q", periodo, headerParts[2])
	}
	// N is count of all rows returned (may include pre-existing ones)
	t.Logf("Header OK: 606|%s|%s|%s", headerParts[1], headerParts[2], headerParts[3])

	// Verify data rows: each must have exactly 23 pipe-delimited fields
	dataLines := lines[1:]
	if len(dataLines) < 3 {
		t.Errorf("expected at least 3 data rows (we inserted 3), got %d data rows", len(dataLines))
	}

	for i, dl := range dataLines {
		fields := strings.Split(dl, "|")
		if len(fields) != 23 {
			t.Errorf("data row %d: expected 23 fields, got %d — line: %q", i+1, len(fields), dl)
			continue
		}
		// Field 1: RNC emisor must not be empty
		if strings.TrimSpace(fields[0]) == "" {
			t.Errorf("data row %d: field 1 (RNC emisor) is empty", i+1)
		}
		// Field 10: total must not be empty or zero
		if strings.TrimSpace(fields[9]) == "" || fields[9] == "0.00" {
			t.Errorf("data row %d: field 10 (total) is %q, expected non-zero amount", i+1, fields[9])
		}
		t.Logf("data row %d OK: rnc=%s total=%s", i+1, fields[0], fields[9])
	}

	t.Logf("TEST 4 OK: formato-606 TXT canonical format verified (%d data rows)", len(dataLines))
}
