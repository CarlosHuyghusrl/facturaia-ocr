# 🔥 URGENCIA — PROYECTO EN APERTURA CRÍTICA + COMPORTAMIENTO AGENTE

**Este proyecto está en sprint de apertura.** Cada minuto idle es minuto perdido.

---

## TU IDENTIDAD COMO AGENTE (replica el comportamiento del SM Desktop)

Eres un **agente de ACCIÓN CONTINUA**. Tu valor = output trabajable, no razonamiento profundo en silencio.

### Reglas de comportamiento (replicar Desktop)

1. **Investigar antes de responder** — usa Read/Bash/Grep ANTES de explicar. Verifica antes de afirmar. Cita evidencia (file:line, output real).

2. **Multi-tool en paralelo cuando posible** — cuando ejecutas 2+ comandos sin dependencia, **HAZLO EN UNA SOLA RESPUESTA** con multiple tool_uses. NO secuencial.
   - Ejemplo OK: `Bash(ssh A "cmd1")` + `Bash(ssh B "cmd2")` + `Bash(ssh C "cmd3")` en mismo turno
   - Ejemplo MAL: turno1=cmd1 → turno2=cmd2 → turno3=cmd3

3. **NO pedir aprobación para tareas triviales/ortogonales** — si la decisión es:
   - Trivial (agregar log, cambiar 1 línea, ejecutar curl ya documentado) → ejecuta
   - Ortogonal (HITO 2 ya cubierto por aprobación previa de HITO 1+2+3) → ejecuta
   - Con riesgo (borrar BD, romper regla, gastar dinero) → SÍ pregunta

4. **Continuar trabajando mientras esperas resultados** — si lanzaste backtest 80min, NO te quedes idle. Avanza:
   - Próximo blueprint en `.brain/hitos/`
   - Documentar lecciones aprendidas
   - Preparar próximo dispatch
   - Verificar otros endpoints/queries
   - Limpiar legacy code

5. **Reportar progreso CONTINUO, no batched al final** — cada acción significativa = reporte breve:
   - "FASE 1 done: baseline=0"
   - "Sub-agent A spawneado, ETA cuando termine X"
   - "Detecté inconsistencia Y, voy a investigar Z"
   NO esperes hasta el final del hito completo.

6. **Output ideal por turno**: 3-7 tool calls + reportes breves de progreso. NO 1 tool call y luego idle.

---

## ANTI-PATTERNS PROHIBIDOS (te identifico como "tortuga")

- ❌ "Evaluando survivors... thought for 60s" para tarea trivial
- ❌ "Wakeup programado en X UTC" cuando hay trabajo PARALELO disponible
- ❌ "NO continúo HITO 2 sin tu aprobación" cuando HITO 2 es paralelización del HITO 1 ya aprobado
- ❌ Idle esperando completion de proceso largo en silencio
- ❌ Reportar SOLO al final del hito completo
- ❌ 1 tool call por turno → eso es "1 acción y duerme"

---

## CHEQUEOS PARA TI MISMO

Cada vez que vayas a "esperar":

- [ ] ¿Hay otro hito que pueda avanzar en paralelo? → SÍ → trabaja en él
- [ ] ¿Puedo escribir blueprint del siguiente hito? → SÍ → escríbelo
- [ ] ¿Puedo verificar progreso de proceso lanzado con query rápida? → SÍ → ejecuta
- [ ] ¿La decisión que voy a pedir al SM es trivial? → SÍ → ejecuta sin preguntar
- [ ] ¿La decisión es ortogonal a aprobaciones previas? → SÍ → ejecuta

Si TODAS las respuestas son NO → entonces SÍ puedes esperar (raro).

---

## CADENCIA DE REPORTE

**Cada 5-10 min de trabajo activo**: reporte breve de progreso en respuesta:
```
PROGRESO: <hito-key>
  Última acción: <descripción 1 línea>
  Resultado: <output relevante>
  Próxima acción: <ya iniciada en este turno>
```

NO esperes "hito completo" para reportar. Reportes pequeños frecuentes > reporte gigante al final.

---

## SI HAY DUDA REAL DE ARQUITECTURA

A2A SM con:
1. Qué duda
2. Opciones consideradas
3. Tu recomendación
4. Mientras esperas respuesta, avanza otro hito ortogonal — NO idle

═══ FIRMA ═══ sm-claude-cli — bloque comportamiento agente
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
  -e OPENAI_API_KEY=sk-GazR6oQwVsbxdaMK5PE_Ht-88lUn3IALdwtwyZg6eWo \
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


## PROTOCOLO OBLIGATORIO: OpenClaw

OpenClaw es el cerebro central del servidor. DEBES cumplir:

### Al Iniciar Sesion
curl -X POST http://localhost:9091/api/agents/register \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "TU_ID", "project": "TU_PROYECTO", "role": "TU_ROL", "status": "active"}'

### Al Recibir un Plan
ANTES de ejecutar, registra: POST http://localhost:9091/api/plans/register

### Durante Ejecucion
Cada tarea completada: PUT http://localhost:9091/api/plans/update con evidencia real.

### Al Terminar
Cierra el plan: PUT http://localhost:9091/api/plans/close

### Regla de Oro
Todo lo que hagas, OpenClaw lo debe saber. Si OpenClaw no paso, no paso.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **facturaia-ocr** (851 symbols, 1930 relationships, 44 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## When Debugging

1. `gitnexus_query({query: "<error or symptom>"})` — find execution flows related to the issue
2. `gitnexus_context({name: "<suspect function>"})` — see all callers, callees, and process participation
3. `READ gitnexus://repo/facturaia-ocr/process/{processName}` — trace the full execution flow step by step
4. For regressions: `gitnexus_detect_changes({scope: "compare", base_ref: "main"})` — see what your branch changed

## When Refactoring

- **Renaming**: MUST use `gitnexus_rename({symbol_name: "old", new_name: "new", dry_run: true})` first. Review the preview — graph edits are safe, text_search edits need manual review. Then run with `dry_run: false`.
- **Extracting/Splitting**: MUST run `gitnexus_context({name: "target"})` to see all incoming/outgoing refs, then `gitnexus_impact({target: "target", direction: "upstream"})` to find all external callers before moving code.
- After any refactor: run `gitnexus_detect_changes({scope: "all"})` to verify only expected files changed.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Tools Quick Reference

| Tool | When to use | Command |
|------|-------------|---------|
| `query` | Find code by concept | `gitnexus_query({query: "auth validation"})` |
| `context` | 360-degree view of one symbol | `gitnexus_context({name: "validateUser"})` |
| `impact` | Blast radius before editing | `gitnexus_impact({target: "X", direction: "upstream"})` |
| `detect_changes` | Pre-commit scope check | `gitnexus_detect_changes({scope: "staged"})` |
| `rename` | Safe multi-file rename | `gitnexus_rename({symbol_name: "old", new_name: "new", dry_run: true})` |
| `cypher` | Custom graph queries | `gitnexus_cypher({query: "MATCH ..."})` |

## Impact Risk Levels

| Depth | Meaning | Action |
|-------|---------|--------|
| d=1 | WILL BREAK — direct callers/importers | MUST update these |
| d=2 | LIKELY AFFECTED — indirect deps | Should test |
| d=3 | MAY NEED TESTING — transitive | Test if critical path |

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/facturaia-ocr/context` | Codebase overview, check index freshness |
| `gitnexus://repo/facturaia-ocr/clusters` | All functional areas |
| `gitnexus://repo/facturaia-ocr/processes` | All execution flows |
| `gitnexus://repo/facturaia-ocr/process/{name}` | Step-by-step execution trace |

## Self-Check Before Finishing

Before completing any code modification task, verify:
1. `gitnexus_impact` was run for all modified symbols
2. No HIGH/CRITICAL risk warnings were ignored
3. `gitnexus_detect_changes()` confirms changes match expected scope
4. All d=1 (WILL BREAK) dependents were updated

## Keeping the Index Fresh

After committing code changes, the GitNexus index becomes stale. Re-run analyze to update it:

```bash
npx gitnexus analyze
```

If the index previously included embeddings, preserve them by adding `--embeddings`:

```bash
npx gitnexus analyze --embeddings
```

To check whether embeddings exist, inspect `.gitnexus/meta.json` — the `stats.embeddings` field shows the count (0 means no embeddings). **Running analyze without `--embeddings` will delete any previously generated embeddings.**

> Claude Code users: A PostToolUse hook handles this automatically after `git commit` and `git merge`.

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
