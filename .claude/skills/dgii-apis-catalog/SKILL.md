---
name: dgii-apis-catalog
description: Catálogo completo de las 24+ APIs DGII en GestoriaRD. URLs, shapes, ejemplos curl, qué tabla BD lee cada una, qué frontend componente la consume. Usar cuando un arquitecto necesite saber qué endpoints tiene disponibles, cuando el diseñador del SaaS necesite saber qué datos puede consumir, o cuando un agente IA tenga que generar código que llame estos endpoints.
---

# DGII APIs — Catálogo para Agentes

Fuente única de verdad para las APIs REST de GestoriaRD que sirven datos del
portal DGII (Dirección General de Impuestos Internos, RD).

**Discovery programático en runtime:**
```bash
curl -s http://localhost:3000/api/v2/dgii/catalog?stats=1 | jq
```

Si añades un endpoint nuevo, **actualiza este archivo + el catalog/route.ts + el README.md** o pierdes la trazabilidad.

## Quick Reference (24 endpoints)

### Formularios fiscales (13)

| Endpoint | Retorna | Tabla BD | Frontend | Status |
|---|---|---|---|---|
| `GET /api/v2/dgii/606/sp-data?rnc&periodo?` | Compras NCF, totales ITBIS | `dgii_606_sp_lines` | `app/dgii/[rnc]/606/page.tsx` | LIVE |
| `GET /api/v2/dgii/607/sp-data?rnc&periodo?` | Ventas NCF emitidos | `dgii_607_sp_lines` | `app/dgii/[rnc]/607/page.tsx` | LIVE |
| `GET /api/v2/dgii/608/sp-data?rnc&periodo?` | NCF anulados con motivo | `dgii_608_sp_lines` | `app/dgii/[rnc]/608/page.tsx` | LIVE |
| `GET /api/v2/dgii/609/sp-data?rnc&periodo?` | Pagos al exterior | `dgii_609_sp_lines` | `app/dgii/[rnc]/609/page.tsx` | LIVE |
| `GET /api/v2/dgii/610/sp-data?rnc&periodo?` | Retenciones servicios | `dgii_610_sp_lines` | `app/dgii/[rnc]/610/page.tsx` | LIVE |
| `GET /api/v2/dgii/623/sp-data?rnc&periodo?` | Retenciones Estado | `dgii_623_sp_lines` | `app/dgii/[rnc]/623/page.tsx` | LIVE |
| `GET /api/v2/dgii/ir1/sp-data?rnc&ejercicio?` | IR-1 persona física | `dgii_ir1_sp_lines` | `app/dgii/[rnc]/ir1/page.tsx` | LIVE |
| `GET /api/v2/dgii/ir2/sp-data?rnc&ejercicio?` | IR-2 persona jurídica + anexos | `dgii_ir2_sp_lines` | `app/dgii/[rnc]/ir2/page.tsx` | LIVE |
| `GET /api/v2/dgii/ir17/sp-data?rnc&periodo?` | IR-17 retenciones asalariados | `dgii_ir17_sp_lines` | `app/dgii/[rnc]/ir17/page.tsx` | LIVE |
| `GET /api/v2/dgii/it1/sp-data?rnc&periodo?` | IT-1 ITBIS (95 casillas) | `dgii_it1_sp_lines` | `app/dgii/[rnc]/it1/page.tsx` | LIVE |
| `GET /api/v2/dgii/act/sp-data?rnc&ejercicio?` | ACT impuesto activos | `dgii_act_sp_lines` | `app/dgii/[rnc]/act/page.tsx` | LIVE |
| `GET /api/v2/dgii/crs/sp-data?rnc&ejercicio?` | CRS reporting | `dgii_crs_sp_lines` | `app/dgii/[rnc]/crs/page.tsx` | LIVE |
| `GET /api/v2/dgii/rst/sp-data?rnc&periodo?` | RST simplificado | `dgii_rst_sp_lines` | `app/dgii/[rnc]/rst/page.tsx` | LIVE |

### Complementarios (5)

| Endpoint | Retorna | Tabla BD | Frontend | Status |
|---|---|---|---|---|
| `GET /api/v2/dgii/pagos/sp-data?rnc` | Histórico pagos DGII | `dgii_pagos` | `app/dgii/[rnc]/pagos/page.tsx` | LIVE |
| `GET /api/v2/dgii/obligaciones-tributarias/sp-data?rnc` | Catálogo obligaciones (~16/cliente) | `dgii_obligaciones_tributarias` | `app/dgii/[rnc]/obligaciones/page.tsx` | LIVE |
| `GET /api/v2/dgii/cuenta-corriente/sp-data?rnc&desde?&hasta?` | Movimientos + saldos + deudas | `dgii_cuenta_corriente` | `app/dgii/[rnc]/cuenta-corriente/page.tsx` | LIVE |
| `GET /api/v2/dgii/ncf-vencimientos?rnc` | NCF activos + próximos vencer | `dgii_ncf_autorizaciones` | `app/dgii/[rnc]/ncf/page.tsx` | LIVE |
| `POST /api/v2/dgii/ai-assistant {rnc?,question}` | RAG sobre 7,186+ chunks fiscales | via `:8322` (rag) | `components/ai/AsistenteFiscal.tsx` | LIVE |

### Operacionales (4)

| Endpoint | Retorna | Tabla BD | Frontend | Status |
|---|---|---|---|---|
| `GET /api/v2/dgii/scraping-status?rnc` | Estado scraping por sección + última corrida | `dgii_scrape_index` | `app/dgii/[rnc]/status/page.tsx` | LIVE |
| `GET /api/v2/dgii/notificaciones/stream?rnc` (SSE) | Stream tiempo real notificaciones | `dgii_notificaciones` | `components/notif/NotificacionesLive.tsx` | LIVE |
| `GET /api/v2/dgii/cuadre?rnc?&severidad?&periodo?` | Discrepancias 606/607 vs IT-1 | `dgii_cuadre_discrepancias` | `app/dgii/cuadre/page.tsx` | LIVE |
| `GET /api/v2/dgii/catalog?stats?` | **Este catálogo** programático | (hardcoded) | — | LIVE |

### Externos / TSS (1)

| Endpoint | Retorna | Tabla BD | Frontend | Status |
|---|---|---|---|---|
| `POST /api/tss/chat` | Chatbot Tesorería Seguridad Social | `tss_data` | `app/tss/page.tsx` | PENDING_DEPLOY |

## Convenciones obligatorias

### Naming
- **`/sp-data`** — endpoint que lee tabla SharePoint-sourced (`dgii_*_sp_lines`)
- **`/stream`** — SSE para tiempo real
- **plain** — orquestación (combina fuentes / no mapea 1-a-1 con tabla)

### RNC normalization
Todo endpoint acepta `131-047939` o `131047939`. Limpia con `.replace(/-/g, '').trim()`.

### Response shape estándar
```ts
// Mensual
{ periodos: string[], periodo_activo: string|null, facturas|retenciones|casillas: object[], totales?, source? }

// Anual
{ ejercicios: string[], ejercicio_activo: string|null, casillas: object[], anexos? }
```

### Auto-fallback periodo
Si no se pasa `periodo`, el endpoint usa el más reciente disponible en BD para ese RNC.

## Ejemplos curl probados

```bash
# 606 último periodo HUYGHU
curl -s 'http://localhost:3000/api/v2/dgii/606/sp-data?rnc=131-047939' | jq '.totales'

# IT-1 marzo 2026
curl -s 'http://localhost:3000/api/v2/dgii/it1/sp-data?rnc=131-047939&periodo=202603' | jq

# Cuadre discrepancias alta
curl -s 'http://localhost:3000/api/v2/dgii/cuadre?severidad=alta' | jq

# AI asistente
curl -s -X POST http://localhost:3000/api/v2/dgii/ai-assistant \
  -H 'Content-Type: application/json' \
  -d '{"rnc":"131-047939","question":"¿cuánto adeudo en ITBIS?"}' | jq

# Catálogo con stats reales
curl -s 'http://localhost:3000/api/v2/dgii/catalog?stats=1' | jq '.endpoints[] | {path, data_count}'

# SSE notificaciones
curl -N 'http://localhost:3000/api/v2/dgii/notificaciones/stream?rnc=131-047939'
```

## Cómo añadir endpoint nuevo

1. **Crea** `app/api/v2/dgii/<nombre>/sp-data/route.ts` siguiendo el template del README en
   `/home/gestoria/gestion-contadoresrd/app/api/v2/dgii/README.md`.
2. **Añade entry** al array `ENDPOINTS` en
   `/home/gestoria/gestion-contadoresrd/app/api/v2/dgii/catalog/route.ts`.
3. **Actualiza** la tabla en este SKILL.md.
4. **Actualiza** la tabla en `app/api/v2/dgii/README.md`.
5. **Verifica** TS check pasa: `cd ~/gestion-contadoresrd && npx tsc --noEmit -p .`
6. **Commit** con `[FEAT] api: <nombre> + catalog/README/skill update`.

Si saltas pasos 2-4 → otros agentes no encontrarán el endpoint y el catalog mentirá.

## Cuándo invocar este skill

- Diseñador SaaS pregunta "¿qué datos DGII tengo disponibles?"
- Otro arquitecto pregunta "¿cómo consumo X desde mi frontend?"
- Worker IA necesita generar código que llame APIs DGII
- Auditoría de coverage: ¿tenemos endpoint para 629? (No → flag para roadmap)
- Refactor de UI: ¿qué endpoints quedan huérfanos sin componente?

## Referencias cruzadas

- **Repo**: `~/gestion-contadoresrd/` (rama `main`)
- **Catalog endpoint**: `app/api/v2/dgii/catalog/route.ts`
- **README**: `app/api/v2/dgii/README.md`
- **KB key**: `dgii-apis-catalog-260425` (project=dgii-scraper, category=reference)
- **Memory Palace**: wing=`dgii-scraper`, room=`apis-catalog`
- **Scraper que puebla las tablas**: `~/dgii-scraper/` (rama main, repo `CarlosHuyghusrl/dgii-scraper`)
- **DB**: PostgreSQL `217.216.48.91:5433/postgres` (poblada async)

## Roadmap detectado por gap

Endpoints aún NO implementados (según el roadmap del scraper):
- **629** retenciones tarjetas crédito — pendiente loader scraper
- **e-CF electrónicos** — parcial, sin endpoint dedicado
- **TSS/SUIR** — `/api/tss/chat` PENDING_DEPLOY
- **Validación RNC pública** — sin endpoint, requiere fetcher público
- **Tasas de cambio DGII** — sin endpoint
- **Calendario fiscal** — sin endpoint dedicado
