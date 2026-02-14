# FacturaIA Backend - OCR Service

**Version:** v2.13.2
**Lenguaje:** Go 1.24
**Puerto:** 8081
**Path:** ~/factory/apps/facturaia-ocr

---

## Stack

| Componente | Tecnología |
|------------|------------|
| Runtime | Go 1.24 |
| Router | gorilla/mux 1.8.1 |
| Database | pgx/v5 5.8.0 (PostgreSQL) |
| Storage | minio-go/v7 7.0.97 |
| AI Provider | Claude Opus 4.5 via CLIProxyAPI |
| OCR Fallback | Tesseract 5.5.1 |
| JWT | golang-jwt/v5 |

---

## AI Provider

- **Provider:** openai-compatible (CLIProxyAPI)
- **Base URL:** http://localhost:8317/v1
- **Model:** claude-opus-4-5-20251101
- **Vision Mode:** Habilitado (imagen directa sin Tesseract)
- **Fallback:** Gemini 2.5 Flash

---

## Endpoints

| Method | Path | Auth | Descripción |
|--------|------|------|-------------|
| POST | /api/login | No | Autenticación cliente (RNC+PIN) |
| POST | /api/process-invoice | JWT | Procesar factura con OCR |
| GET | /api/facturas/mis-facturas | JWT | Listar facturas del cliente |
| GET | /api/facturas/{id} | JWT | Detalle de factura |
| GET | /api/facturas/{id}/imagen | No* | Proxy imagen desde MinIO |
| DELETE | /api/facturas/{id} | JWT | Eliminar factura |
| GET | /api/facturas/resumen | JWT | Estadísticas del cliente |
| GET | /health | No | Health check |

*UUID no adivinable protege el acceso

---

## Base de Datos

- **DB:** PostgreSQL 16 via PgBouncer (localhost:5433)
- **Tablas:**
  - `facturas_clientes` (26 registros)
  - `facturas` (1 legacy, no usada)

### Campos DGII Extraídos

**Base:**
- subtotal, descuento, monto

**ITBIS:**
- itbis, itbis_retenido, itbis_exento, itbis_proporcionalidad, itbis_costo

**ISR:**
- isr, retencion_isr_tipo (códigos 1-8)

**ISC:**
- isc, isc_categoria (seguros, telecom, alcohol, tabaco, vehículos)

**Otros:**
- cdt_monto (2% telecom), cargo_911, propina, otros_impuestos, monto_no_facturable

---

## Storage

- **Provider:** MinIO
- **Endpoint:** localhost:9000
- **Bucket:** facturas
- **Access:** gestoria_minio
- **SSL:** false

---

## Estructura

```
.
├── cmd/server/         # main.go
├── api/
│   ├── handler.go      # Routes + ProcessInvoice
│   └── client_handlers.go  # Client CRUD + Image proxy
├── internal/
│   ├── models/         # invoice.go
│   ├── db/             # client_invoices.go
│   ├── ai/             # extractor.go (Claude/Gemini)
│   ├── auth/           # JWT middleware
│   ├── storage/        # MinIO client
│   └── ocr/            # Tesseract wrapper
└── go.mod
```

---

## Bugs Conocidos

### ISC = 0 en facturas antiguas
- **Afecta:** Facturas procesadas antes de v2.13.2
- **Cantidad:** 23 de 26 facturas
- **Causa:** Faltaba `&inv.ISCCategoria` en Scan de GetClientInvoiceByID
- **Fix:** v2.13.2 corrige nuevas facturas
- **Pendiente:** Reprocesar facturas antiguas (plan-003)

---

## Deploy

### Build
```bash
cd ~/factory/apps/facturaia-ocr
go build -o facturaia-ocr ./cmd/server
docker build -t facturaia-ocr:v2.13.2 .
```

### Run
```bash
docker run -d --name facturaia-ocr --restart unless-stopped --network host \
  -e PORT=8081 -e HOST=0.0.0.0 \
  -e AI_PROVIDER=openai \
  -e OPENAI_API_KEY=sk-7mFaCRaXj5sp1S5G82S17sF4ClsTzn0ObP1D8yzPEQYmZ \
  -e OPENAI_BASE_URL=http://localhost:8317/v1 \
  -e OPENAI_MODEL=claude-opus-4-5-20251101 \
  -e DATABASE_URL=postgres://postgres:***@localhost:5433/postgres?sslmode=disable \
  -e MINIO_ENDPOINT=localhost:9000 \
  -e MINIO_ACCESS_KEY=gestoria_minio \
  -e MINIO_SECRET_KEY=*** \
  -e MINIO_USE_SSL=false \
  -e MINIO_BUCKET=facturas \
  -e JWT_SECRET=facturaia-jwt-secret-2025-production \
  facturaia-ocr:v2.13.2
```

---

## Test User

- **RNC:** 130309094
- **PIN:** 1234
- **Razón Social:** Acela Associates
