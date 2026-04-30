# 606 Field Mapping — OCR → DGII Schema

**Versión OCR (container)**: `facturaia-ocr:v2.27.0`
**Versión OCR (código `cmd/server/main.go`)**: `v2.25.0` (string log; container retag a v2.27.0)
**Schema DGII canónico**: `dgii_606_sp_lines` (25 columnas oficiales DGII RD)
**Tabla local destino**: `public.facturas_clientes` (Postgres, 60 columnas)
**Loader 606**: `internal/db/client_invoices.go::GetFormato606Invoices` (filtra `aplica_606 = true`, agrupa por `to_char(fecha_documento,'YYYYMM') = $periodo`)
**Fecha audit**: 2026-04-30
**Auditor**: Frente B (audit gaps OCR vs canónico)

---

## 1. Mapeo completo (25 columnas canónicas DGII 606)

Leyenda Estado:
- `OK` = extraído por OCR/IA y persistido tal cual
- `RENAME` = extraído por OCR pero con nombre distinto al canónico (sólo etiquetado)
- `AUTO-DB` = no extraído por OCR; calculado por trigger Postgres `trg_auto_tag_606`
- `MANUAL` = no extraído ni calculado; requiere captura manual o lógica nueva
- `DERIVADO` = se deriva de otro campo extraído (lógica trivial)

| # | Columna DGII canónica | Campo OCR (`models.Invoice`) | Columna BD (`facturas_clientes`) | Estado | Notas |
|---|---|---|---|---|---|
| 1 | `rnc_proveedor` | `RNCEmisor` (`rncEmisor`) | `emisor_rnc` | RENAME | OCR lo extrae limpio (sin guiones). Loader 606 lo emite como `EmisorRNC`. |
| 2 | `tipo_identificacion` | `TipoIDEmisor` (`tipoIdEmisor`) | `tipo_id_emisor` | OK | "1"=RNC, "2"=Cédula. Default `'1'`. |
| 3 | `tipo_bien_servicio` | `TipoBienServicio` (`tipoBienServicio`) | `tipo_bien_servicio` | OK | Códigos 01–13 DGII. Extraído por prompt Gemini. |
| 4 | `ncf` | `NCF` (`ncf`) | `ncf` | OK | B01/B02/B04/B11/B14/B15/E31/E32/E33. |
| 5 | `ncf_modifica` | `NCFModifica` (`ncfModifica`) | `ncf_modifica` | OK | OBLIGATORIO si tipoNcf ∈ {B04, E32, E33}. |
| 6 | `fecha_comprobante` | `FechaFactura` (`fechaFactura`) | `fecha_documento` | RENAME | OCR `YYYY-MM-DD` → BD `date`. |
| 7 | `fecha_pago` | `FechaPago` (`fechaPago`) | `fecha_pago` | OK | Extraído sólo si hay retenciones ITBIS/ISR. |
| 8 | `monto_servicios` | `MontoServicios` (`montoServicios`) | `monto_servicios` | OK | Prompt Gemini divide bienes vs servicios. |
| 9 | `monto_bienes` | `MontoBienes` (`montoBienes`) | `monto_bienes` | OK | Idem. |
| 10 | `total_facturado` | `Subtotal` (`subtotal`) o `Total` (`total`) | `subtotal` (col separada) o `monto` | RENAME | DGII 606 usa "monto facturado" = base imponible (subtotal). En BD se guarda **además** `monto` (total con impuestos). El loader 606 (`GetFormato606Invoices`) emite `Subtotal`. |
| 11 | `itbis_facturado` | `ITBIS` (`itbis`) | `itbis` | OK | 18% normal o 16% zona franca. |
| 12 | `itbis_retenido` | `ITBISRetenido` (`itbisRetenido`) | `itbis_retenido` | OK | + `ITBISRetenidoPorcentaje` (30/100). |
| 13 | `itbis_proporcionalidad` | `ITBISProporcionalidad` (`itbisProporcionalidad`) | `itbis_proporcionalidad` | OK | Art. 349 — gastos mixtos. |
| 14 | `itbis_costo` | `ITBISCosto` (`itbisCosto`) | `itbis_costo` | OK | ITBIS no deducible. |
| 15 | `itbis_adelantar` | — (no extraído) | `itbis_adelantar` | AUTO-DB | Trigger lo calcula: `GREATEST(0, itbis - itbis_retenido - itbis_proporcionalidad - itbis_costo)` cuando llega NULL/0. |
| 16 | `itbis_percibido` | — (no extraído por OCR) | `itbis_percibido` | MANUAL | Aplica sólo a NCF B11/B12 (agentes designados percepción). Trigger no lo calcula. Frontend debe permitir captura manual. INSERT lo persiste (ver `SaveClientInvoice` línea 382/422). |
| 17 | `tipo_retencion_isr` | `RetencionISRTipo` (`retencionIsrTipo`) | `retencion_isr_tipo` | RENAME | Códigos 1–8 DGII. `*int` (nullable). |
| 18 | `monto_retencion_renta` | `ISR` (`isr`) | `isr` | RENAME | OCR lo etiqueta como "ISR retenido" = "monto retención renta" del 606. |
| 19 | `isr_percibido` | — (no extraído por OCR) | `isr_percibido` | MANUAL | Análogo a itbis_percibido. Sólo agentes percepción ISR. Persistido por `SaveClientInvoice`. |
| 20 | `isc` | `ISC` (`isc`) | `isc` | OK | + `ISCCategoria` (auxiliar, no DGII). |
| 21 | `otros_impuestos` | `OtrosImpuestos` (`otrosImpuestos`) | `otros_impuestos` | OK | También captura `CDTMonto` y `Cargo911` por separado en BD pero NO los suma a este campo (gap menor — ver §3). |
| 22 | `propina_legal` | `Propina` (`propina`) | `propina` | RENAME | OCR la captura como "propina"; canónico DGII la llama "propina_legal" (10%). |
| 23 | `forma_pago` | `FormaPago` (`formaPago`) | `forma_pago` | OK | Códigos 01–07 DGII. |
| 24 | `aplica_606` (flag interno, no es columna DGII pero lo necesita el loader) | — | `aplica_606` | AUTO-DB | Trigger `trg_auto_tag_606` consulta `dgii_ncf_tipos.aplica_606` por prefijo NCF. |
| 25 | `periodo_606` (flag interno) | — | `periodo_606` | AUTO-DB | Trigger calcula `to_char(fecha_documento,'YYYYMM')`. |

> **Nota sobre el conteo "25 columnas"**: el formato DGII oficial 606 tiene **23 columnas en el archivo TXT** (rnc_proveedor → forma_pago). Las 2 últimas (`aplica_606`, `periodo_606`) son flags internos del sistema FacturaIA — no van al TXT — pero se enumeran porque son la lógica que decide qué fila exporta el loader.

---

## 2. Resumen cuantitativo

- **Campos OCR struct (`models.Invoice`)**: ~80 (incluyendo legacy `Vendor/Date/Total/Tax`, items, raw, metadata).
- **Claves JSON en prompt Gemini**: ~68 distintas.
- **Columnas en `facturas_clientes`**: 60.
- **Columnas DGII canónicas 606**: 23 oficiales + 2 flags internos = 25.
- **Cobertura OCR → 606**:
  - Extraídos directamente por IA: **20 / 23** oficiales (87%).
  - Calculados por trigger Postgres: **2** (`itbis_adelantar`, `aplica_606`/`periodo_606` lógica).
  - **Gap real**: **2 campos** que no se extraen ni se calculan: `itbis_percibido`, `isr_percibido`.

> El audit anterior decía "20/23 + faltan itbis_percibido, isr_percibido, aplica_606". Esta auditoría **corrige**: `aplica_606` SÍ está implementado vía trigger Postgres `trg_auto_tag_606` (verificado en BD productiva). Faltan solo `itbis_percibido` e `isr_percibido`.

---

## 3. Gaps identificados

### 3.1 Faltantes reales (extractor IA no implementado, trigger no los calcula)

#### `itbis_percibido` — MANUAL
- **Origen DGII**: ITBIS percibido por agentes designados al cobrar facturas con NCF B11 (Régimen Especial) o B12 (Gubernamentales).
- **Estado actual**: columna existe en BD (`itbis_percibido NUMERIC(12,2) DEFAULT 0`), `SaveClientInvoice` la persiste (parámetro $49), loader 606 la lee (`Formato606Invoice.ITBISPercibido`). PERO ni el prompt Gemini ni el trigger la calculan. Siempre llega 0.
- **Impacto**: Si la app emite facturas a clientes en régimen especial, el TXT 606 saldrá con columna vacía → DGII puede rechazar o el contador debe corregirlo.
- **Prevalencia**: BAJA — sólo aplica a contribuyentes que son agentes designados de percepción (lista DGII corta).

#### `isr_percibido` — MANUAL
- **Origen DGII**: análogo a itbis_percibido pero para ISR.
- **Estado actual**: idéntico a itbis_percibido — columna en BD + persistencia OK + loader OK + extracción/cálculo NO implementado.
- **Impacto**: idem. Prevalencia BAJA.

### 3.2 Cobertura OK pero requiere confirmación manual del usuario

- `monto_servicios` / `monto_bienes`: OCR los extrae pero la división servicios vs bienes en facturas mixtas es heurística IA — recomendable que el contador valide en UI antes de generar TXT 606.
- `tipo_bien_servicio`: códigos 01–13. La IA puede equivocarse en categorización.

### 3.3 Inconsistencias menores (extras OCR no en schema canónico, no bloquean)

OCR extrae pero NO van al TXT 606 (se guardan en BD para otras funciones / IT-1):
- `descuento`, `itbis_exento`, `monto_no_facturable` — IT-1 y validaciones, no 606.
- `cdt_monto`, `cargo_911` — telecom; no son "otros_impuestos" 606 stricto sensu, debate si sumarlos a `otros_impuestos` o dejarlos separados (actualmente separados).
- `isc_categoria` — taxonomía interna, no DGII.
- `itbis_tasa`, `itbis_retenido_porcentaje` — auxiliares de cálculo.
- `confidence_score`, `raw_ocr_json`, `items_json`, `extraction_status`, `review_notes` — metadata IA.
- `hora_factura` — usado para deduplicación (`CheckDuplicateByAmount`), no DGII.
- `ncf_vencimiento` — IT-1, no 606.
- `aplica_607`, `periodo_607` — flag análogo para Formato 607 (ventas).

**Recomendación**: dejarlos donde están (storage actual). NO descartar — son útiles para IT-1, deduplicación, auditoría IA.

---

## 4. Verificación: trigger `trg_auto_tag_606` (CONFIRMADO en producción)

```sql
-- Verificado en supabase-db (puerto 5433):
SELECT tgname, pg_get_triggerdef(oid)
FROM pg_trigger
WHERE tgrelid = 'facturas_clientes'::regclass AND NOT tgisinternal;

-- Resultado:
-- trg_auto_tag_606 BEFORE INSERT OR UPDATE ON public.facturas_clientes
-- FOR EACH ROW EXECUTE FUNCTION auto_tag_factura_606()
```

Lógica del trigger (resumen):
1. Toma prefijo del NCF (`upper(left(NEW.ncf, 3))`).
2. Consulta `dgii_ncf_tipos.aplica_606` por prefijo (fuente de verdad).
3. Setea `NEW.aplica_606 := v_aplica` (default false si prefijo desconocido).
4. Calcula `NEW.periodo_606 := to_char(NEW.fecha_documento, 'YYYYMM')`.
5. Calcula `NEW.itbis_adelantar := GREATEST(0, itbis - itbis_retenido - itbis_proporcionalidad - itbis_costo)` si llega NULL/0.

**Conclusión**: `aplica_606` automático YA EXISTE y funciona. No requiere trabajo adicional. El audit previo "20/23 faltan aplica_606 automático" **estaba desactualizado**.

---

## 5. Plan para los 2 campos faltantes (`itbis_percibido`, `isr_percibido`)

### Opción A — Captura manual UI (recomendada, mínimo riesgo)
- Agregar dos inputs en pantalla de edición de factura (frontend RN/web).
- Mostrarlos sólo cuando NCF ∈ {B11, B12} (agentes designados).
- Default 0. Endpoint update ya los persiste si llegan.
- **Esfuerzo**: 1 día frontend, 0 backend.

### Opción B — Lógica determinística en trigger (riesgo medio)
- Agregar al trigger `auto_tag_factura_606`:
  ```sql
  -- Si NCF es B11 y receptor está en lista de agentes percepción ITBIS:
  IF v_prefijo = 'B11' THEN
    SELECT porcentaje_percepcion INTO v_pct
    FROM dgii_agentes_percepcion_itbis
    WHERE rnc = NEW.receptor_rnc;
    IF v_pct IS NOT NULL THEN
      NEW.itbis_percibido := NEW.itbis * v_pct / 100;
    END IF;
  END IF;
  ```
- Requiere tabla `dgii_agentes_percepcion_itbis` mantenida.
- **Esfuerzo**: 2 días (migration + scrape DGII de lista oficial + tests).

### Opción C — Extracción por IA (no recomendada)
- Pedir al prompt Gemini detectar "ITBIS percibido". Casi nunca aparece en facturas físicas (es un cálculo del receptor, no del emisor). Falsos positivos altos.

**Recomendación de prioridad (§11)**: **Opción A**. Los agentes percepción son <50 RNC en RD, prevalencia muy baja. Captura manual + validación contador es suficiente para Sprint 1. Reservar Opción B para cuando haya métricas reales de cuántas facturas/mes lo necesitan.

---

## 6. Schema actual `facturas_clientes` (60 columnas)

```
id (uuid PK), cliente_id (uuid FK→clientes), empresa_id (uuid),
archivo_url, archivo_nombre, archivo_size, tipo_documento, fecha_documento,
monto NUMERIC(12,2), ncf, proveedor, estado, notas_cliente, notas_contador,
procesado_por, procesado_at, created_at,
emisor_rnc VARCHAR(20), receptor_nombre, receptor_rnc,
subtotal, itbis, itbis_retenido, isr, propina, otros_impuestos,
forma_pago, tipo_ncf, tipo_bien_servicio,
confidence_score NUMERIC(5,4), raw_ocr_json JSONB, items_json JSONB,
isc, descuento, itbis_exento, monto_no_facturable, ncf_vencimiento,
cdt_monto, cargo_911, itbis_proporcionalidad, itbis_costo,
retencion_isr_tipo SMALLINT, extraction_status, review_notes, isc_categoria,
itbis_tasa, fecha_pago, ncf_modifica, tipo_id_emisor, tipo_id_receptor,
monto_servicios, monto_bienes, itbis_retenido_porcentaje,
itbis_adelantar, itbis_percibido, isr_percibido,
aplica_606 BOOL, periodo_606 VARCHAR(6),
hora_factura VARCHAR(5),
aplica_607 BOOL, periodo_607 VARCHAR(6)
```

Índices clave para 606:
- `idx_facturas_aplica_606 (empresa_id, periodo_606) WHERE aplica_606 = true` — partial index optimiza loader.
- `idx_facturas_periodo_606 (periodo_606)` — fallback.
- `idx_facturas_clientes_ncf_empresa UNIQUE (ncf, empresa_id)` — anti-duplicado NCF.

---

## 7. Recomendación final (juicio §11)

**Estado real del frente OCR → 606: 91% completo (21 de 23 columnas DGII funcionando end-to-end).**

Prioridades:

1. **NO TOCAR código Go**. La extracción IA + persistencia + trigger BD están correctos.
2. **Cerrar gap UI**: añadir inputs `itbis_percibido` / `isr_percibido` en pantalla de edición factura (frontend), visibles sólo para NCF B11/B12. (Opción A §5).
3. **Documentar para contador**: que `monto_servicios` / `monto_bienes` y `tipo_bien_servicio` son sugerencias IA y debe validarlos antes de generar TXT 606.
4. **No requerido para Sprint actual**: tabla `dgii_agentes_percepcion_itbis` (Opción B). Esperar a tener volumen real.
5. **Verificación end-to-end pendiente**: generar un TXT 606 de prueba con `internal/services/formato_606.go` (no auditado aquí — fuera de scope) y validarlo contra el validador oficial DGII.

---

## 8. Referencias de código

| Componente | Path absoluto |
|---|---|
| OCR struct principal | `/home/gestoria/factory/apps/facturaia-ocr/internal/models/invoice.go` (líneas 10–85) |
| Prompt Gemini imagen | `/home/gestoria/factory/apps/facturaia-ocr/internal/ai/extractor.go` (líneas 80–227) |
| Prompt Gemini OCR-text | `/home/gestoria/factory/apps/facturaia-ocr/internal/ai/extractor.go` (líneas 229–340 aprox) |
| BD struct ClientInvoice | `/home/gestoria/factory/apps/facturaia-ocr/internal/db/client_invoices.go` (líneas 13–80) |
| Persist INSERT | `/home/gestoria/factory/apps/facturaia-ocr/internal/db/client_invoices.go::SaveClientInvoice` (línea 362) |
| Loader 606 | `/home/gestoria/factory/apps/facturaia-ocr/internal/db/client_invoices.go::GetFormato606Invoices` (línea 561) |
| Toggle manual aplica_606 | `/home/gestoria/factory/apps/facturaia-ocr/internal/db/client_invoices.go::ToggleAplica606` (línea 662) |
| Trigger BD | `auto_tag_factura_606()` en supabase-db (`postgres` DB, schema `public`) |
| Tabla referencia | `dgii_ncf_tipos` (prefijo → aplica_606) |
| Container productivo | `facturaia-ocr:v2.27.0` (corriendo en host puerto 8081) |

---

**Documento generado por**: Frente B audit, 2026-04-30.
**No modifica código**. Sólo análisis + plan.
