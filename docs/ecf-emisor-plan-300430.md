# e-CF Emisor — Plan Implementación (2026-04-28 → deadline 2026-05-15)

**Deadline DGII**: 15 Mayo 2026 (17 días naturales / ~12 días hábiles desde hoy)
**Beta**: HUYGHU SRL única (RNC 131047939, empresa_id `616b8f1b-d3f1-403d-883b-aec3302363e5`, registrada como cliente `Huyghu y ASOC.`)
**Estado actual repo**: NO existe capability emisora e-CF en facturaia-ocr (verificado por grep + go.mod)

---

## 1. Spec DGII e-CF resumida (v1.0 oct-2025)

### 1.1 Tipos de e-CF (DGII RD)

| Código | Nombre | Equivalente NCF |
|---|---|---|
| **31** | Factura de Crédito Fiscal Electrónica | B01 |
| **32** | Factura de Consumo Electrónica | B02 |
| **33** | Nota de Débito Electrónica | B03 |
| **34** | Nota de Crédito Electrónica | B04 |
| **41** | Compras Electrónica (proveedor informal) | B11 |
| **43** | Gastos Menores Electrónica | B13 |
| **44** | Regímenes Especiales Electrónica | B14 |
| **45** | Gubernamental Electrónica | B15 |
| **46** | Exportaciones Electrónica | B16 |
| **47** | Pagos Exterior Electrónica | B17 |

> Para HUYGHU SRL (despacho contable B2B) los tipos críticos son **31** (crédito fiscal a clientes contribuyentes) y **32** (consumo a clientes finales). 33/34 son notas relacionadas. El resto puede esperar a fase 2.

### 1.2 Esquema XML (XSD)

- DGII publica esquemas XSD para cada tipo (31, 32, 33, 34, RFCE Resumen Factura, ACECF Acuse, ANECF Anulación). Última actualización pública: **octubre 2025**.
- Documento maestro: [Formato Comprobante Fiscal Electrónico (e-CF) v1.0 — DGII PDF](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Documentacin%20sobre%20eCF/Formatos%20XML/Formato%20Comprobante%20Fiscal%20Electr%C3%B3nico%20(e-CF)%20v1.0.pdf)
- Página índice: [Documentación e-CF DGII](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Paginas/documentacionSobreE-CF.aspx)

### 1.3 Firma digital

- **XAdES-BES** sobre el XML completo del e-CF antes de envío.
- Algoritmo hash: SHA-256.
- Canonicalización: C14N exclusiva.
- Certificado: X.509 emitido por una **autoridad certificadora autorizada DGII** (típicamente Avansi, Camara TIC, o el certificado mediante el sistema de firma digital de DGII). Token físico USB o software (.p12/.pfx).
- Cada e-CF debe llevar su firma incrustada antes del POST a DGII.

### 1.4 Endpoints DGII

| Ambiente | Recepción | Consulta resultado |
|---|---|---|
| TesteCF (pre-cert) | `https://ecf.dgii.gov.do/testecf/recepcion/api/facturaselectronicas` | `https://ecf.dgii.gov.do/testecf/consultaresultado/api/consultas/estado?trackid={id}` |
| CerteCF (certificación) | `https://ecf.dgii.gov.do/certecf/recepcion/api/facturaselectronicas` | `https://ecf.dgii.gov.do/certecf/consultaresultado/api/consultas/estado?trackid={id}` |
| eCF (producción) | `https://ecf.dgii.gov.do/ecf/recepcion/api/facturaselectronicas` | `https://ecf.dgii.gov.do/ecf/consultaresultado/api/consultas/estado?trackid={id}` |

- Auth: **Bearer JWT** obtenido vía endpoint de autenticación con seed firmada (semilla → firma con cert privado → DGII responde JWT).
- Respuesta inmediata: **trackID** + estado preliminar.
- Acuse final: polling al endpoint de consultas hasta obtener "Aceptado", "Rechazado" o "Aceptado Condicional".

### 1.5 Resumen de Factura (RFCE) y Anulaciones (ANECF)

Cada e-CF emitido debe ir acompañado por un Resumen de Factura (XML adicional firmado) que se envía a DGII para registro contable. Las anulaciones requieren XML ANECF firmado.

---

## 2. Estado actual del repo

### 2.1 Código Go
- **Cero** archivos `.go` con referencias a `e-CF`, `ECF`, `XAdES`, `firma_digital`, `comprobante_electronico`. Verificado:
  ```
  grep -rln -E "e-?CF|ECF|XAdES" /home/gestoria/factory/apps/facturaia-ocr --include="*.go" → 0 matches en /internal /cmd /api
  ```
- Único hit: `.claude/skills/dgii-apis-catalog/SKILL.md` (mención conceptual, no implementación).

### 2.2 Dependencias Go (go.mod)
- **Cero** librerías de firma XML / XAdES / xmlsig importadas.
- Solo `golang.org/x/crypto` (bajo nivel, indirect) y `crypto/x509` standard library.
- Falta importar: librería XAdES + cliente HTTP a DGII (puede ser `net/http` standard).

### 2.3 Base de datos PostgreSQL
- **3 tablas e-CF detectadas** pero son **scrapping inbox** (snapshots de OFV para ver qué emitió/recibió cada cliente), NO emisión:
  - `dgii_ecf_emitidos` (8 columnas: id, entidad_rnc, tipo_ecf, datos_fila jsonb, scraped_at, content_hash, source_scrape_index_id) — **0 filas actuales**
  - `dgii_ecf_recibidos` (mismo shape) — **0 filas**
  - `dgii_certificaciones` (id, entidad_rnc, tipo_certificacion, fecha_solicitud, estado, fecha_expira, datos_fila) — 21 filas pero **todas con metadata vacía** (solo entidad_rnc poblado)
- HUYGHU está en `clientes` como `Huyghu y ASOC.` RNC `131047939` (NO 130309094 que es Acela Associates, el test user).
- **NO hay** columnas en `empresas` ni `clientes` para `cert_digital_id`, `cert_path`, `cert_password_encrypted`, `cert_expires_at`, etc.
- **NO hay** tabla emisión real (`ecf_outbox`, `ecf_acuses`, etc).

### 2.4 Stack actual del backend
- Go 1.24, gorilla/mux, pgx/v5, MinIO, Gemini Vision (OCR).
- Servicio enfocado 100% en **leer** facturas (OCR) y archivar. Cero capabilities de **emitir** documentos fiscales.

---

## 3. Requisitos previos HUYGHU (bloqueantes humanos)

### 3.1 Certificado digital DGII

- HUYGHU SRL **NO tiene certificado digital activo verificable en BD** (`dgii_certificaciones` con metadata vacía).
- **Trámite**: Carlos debe verificar en OFV DGII si hay certificado vigente. Si no:
  - Solicitar certificado a autoridad certificadora autorizada (Avansi, Camara TIC, etc).
  - Tiempo trámite: **5–10 días hábiles** mínimo (dependiendo CA).
  - Costo aprox: RD$3,500–8,000 anual.

### 3.2 Adhesión al sistema e-CF DGII

- Carlos debe completar el **Proceso de Certificación de Emisor Electrónico** ante DGII:
  - Documentación oficial: [Proceso de Certificación Emisor Electrónico DGII PDF](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Documentacin%20sobre%20eCF/Documentaciones%20Proceso%20de%20Certificaci%C3%B3n%20FE/Proceso%20de%20Certificacion%20para%20ser%20Emisor%20Electronico.pdf)
  - Pasos: solicitud de adhesión, designación de proveedor tecnológico (auto-emisor o tercero), pruebas en TesteCF + CerteCF, autorización formal DGII para pasar a producción.
  - Tiempo certificación DGII: **10–30 días hábiles** dependiendo de turnos y volumen de pruebas exigidas.

### 3.3 Clasificación HUYGHU

- HUYGHU SRL es un despacho contable. Si está clasificada como **Pequeño Contribuyente / Microempresa / No Clasificado** según DGII → **deadline 15-mayo-2026 aplica**.
- Si está como **Mediano** → la prórroga oficial fue **15-noviembre-2025** (ya vencida; HUYGHU estaría en multa).
- Si está como **Grande Local** → 15-mayo-2024 (ya muy vencida).
- **Verificación obligatoria Carlos**: consultar [Listados de Contribuyentes Obligados DGII](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Paginas/Listados-contribuyentes-obligados-implementar-facturacion-electronica.aspx) buscando RNC 131047939.

---

## 4. Estimación esfuerzo (sin bloqueantes humanos)

| Componente | Días-dev | Riesgo |
|---|---|---|
| Schema BD: tablas `ecf_outbox`, `ecf_acuses_dgii`, `ecf_certificados_emisor`, ampliar `facturas_clientes` con columnas `ecf_*` | 1 | bajo |
| Templates Go XML para tipos 31, 32, 33, 34 (text/template + structs serializables) | 2 | medio (XSD complejo, ~80 campos) |
| Firma XAdES-BES en Go (usando `github.com/artemkunich/goxades` + `goxmldsig`) — adaptar canonicalización C14N exclusiva DGII | 3 | **alto** (libs no maintained, sin garantía de compatibilidad DGII; puede requerir port de Node.js [`victors1681/dgii-ecf`](https://github.com/victors1681/dgii-ecf)) |
| Cliente HTTP DGII (auth seed-JWT, POST recepción, polling consulta) | 1 | bajo (API REST documentada) |
| Endpoint backend `POST /api/ecf/emitir` (input → templates → firma → envío → guarda outbox + trackID) | 1 | bajo |
| Worker async polling `/api/consultas/estado` para actualizar acuses (cada 30s × 24h) | 1 | bajo |
| Generador Resumen Factura (RFCE) firmado | 1 | medio |
| UI emisión + visualización acuse (en GestoriaRD, no en facturaia-ocr) | 2 | bajo (delegado a otro repo) |
| Testing CERTIFICACIÓN DGII (suite de pruebas exigida por DGII) | 3 | **alto** (DGII puede pedir N rondas) |
| **TOTAL** | **15 días-dev** | |

---

## 5. Bloqueantes / riesgos críticos

### 5.1 Riesgos que SOLOS pueden tumbar el deadline

| # | Riesgo | Probabilidad | Mitigación |
|---|---|---|---|
| R1 | HUYGHU sin certificado digital → +5–10 días hábiles solo para obtenerlo | **alta** | Carlos lo tramita HOY; si ya lo tiene, descartar |
| R2 | Adhesión DGII no iniciada → 10–30 días hábiles certificación | **muy alta** | Iniciar trámite HOY; pedir vía rápida |
| R3 | Lib Go XAdES no compatible con DGII RD → port desde Node.js + 5 días extra | **media** | Plan B: invocar `victors1681/dgii-ecf` (Node.js) como subproceso desde Go |
| R4 | DGII pide N rondas de pruebas en CerteCF antes de autorizar producción | **alta** | Sin control nuestro; depende de carga DGII |
| R5 | HUYGHU clasificada como Mediano (deadline 15-nov-2025 vencido) → multa diaria acumulada | **media** | Verificar clasificación HOY; si vencido, pedir prórroga DGII |
| R6 | Schema XSD cambia entre desarrollo y producción (DGII ha hecho 6 actualizaciones de XSD entre 2023-2025) | **baja** | Pinear versión de XSD usada y suscribirse a notificaciones DGII |

### 5.2 Riesgos secundarios

- Logs de auditoría: cada e-CF emitido debe ser archivado 10 años (regulación fiscal RD).
- Plan de contingencia DGII caída: protocolo de DGII para emisión offline + sincronización posterior (debe implementarse).
- Multi-tenant futuro: si en fase 2 entran más clientes (no solo HUYGHU), tabla `ecf_certificados_emisor` debe soportar 1 cert por empresa.

---

## 6. Plan tentativo 17 días (29-Abr → 15-May 2026)

| Día | Fecha | Actividad | Owner | Bloqueado por |
|---|---|---|---|---|
| 1 | 29-Abr Mié | Carlos verifica clasificación HUYGHU + estado certificado digital + inicia adhesión DGII | Carlos | — |
| 2 | 30-Abr Jue | Carlos solicita certificado si falta + diseño schema BD `ecf_*` (planificado, no aplicado) | Carlos + arquitecto | día 1 |
| 3 | 1-May Vie | Migración BD: tablas `ecf_outbox`, `ecf_acuses_dgii`, `ecf_certificados_emisor` + columnas en `facturas_clientes` | facturaia | día 2 |
| 4-5 | 2-3 May (sáb-dom) | Buffer / fin de semana / Carlos trámites en paralelo | Carlos | — |
| 6 | 4-May Lun | Templates Go XML tipos 31, 32 (los críticos B2B HUYGHU) | facturaia | día 3 |
| 7 | 5-May Mar | Templates 33, 34 + RFCE | facturaia | día 6 |
| 8-10 | 6-8 May Mié-Vie | Firma XAdES-BES Go + tests unitarios contra XSD | facturaia | cert disponible (R1) |
| 11 | 9-May Sáb | Cliente HTTP DGII (auth seed-JWT) + endpoint `/api/ecf/emitir` | facturaia | día 8-10 |
| 12 | 10-May Dom | Worker polling acuses + UI emisión en GestoriaRD | facturaia + gestoriard | día 11 |
| 13 | 11-May Lun | **Pruebas en TesteCF** (pre-cert sandbox DGII) | facturaia | sistema funcional end-to-end |
| 14-15 | 12-13 May Mar-Mié | **Pruebas en CerteCF** (certificación DGII oficial) | facturaia + Carlos | adhesión DGII (R2) + día 13 |
| 16 | 14-May Jue | Aprobación DGII → switch a producción `eCF` | DGII | DGII responde |
| 17 | 15-May Vie | Go-live HUYGHU emite primer e-CF real | todos | día 16 |

**Sin holgura. Cualquier slip de R1, R2 o R4 hace imposible cumplir.**

---

## 7. Recomendación HONESTA (§11 obligatoria)

### 7.1 Veredicto sobre el deadline 15-mayo-2026

**NO es realista** cumplir el deadline 15-mayo-2026 con desarrollo desde cero. Razones:

1. **17 días naturales** = ~12 días hábiles. Estimación pura de dev son 15 días-dev. Sin contingencia.
2. **Bloqueantes humanos no controlables**:
   - Trámite certificado digital: 5–10 días hábiles si HUYGHU no lo tiene.
   - Proceso certificación DGII: 10–30 días hábiles típico. Solo este paso ya excede el deadline.
3. **Lib XAdES Go sin garantía**: la única lib mantenida (`victors1681/dgii-ecf`) es Node.js. Las libs Go (`goxades`, `goxmldsig`) llevan años sin commits y ninguna está validada contra DGII RD. Riesgo de invertir 3 días y descubrir incompatibilidad.
4. **Pruebas DGII iterativas**: el proceso de certificación CerteCF no tiene SLA garantizado. DGII puede tardar semanas en aprobar una integración nueva con muchas rondas de feedback.
5. **HUYGHU es despacho contable**: si emite e-CF mal formados a clientes con NCF de tipo 31, esos clientes recibirán e-CF rechazados → daño reputacional grave.

### 7.2 Recomendación realista — 3 alternativas

#### Alternativa A (RECOMENDADA): Outsourcing a PSE certificado

- Contratar un **Proveedor de Servicios Electrónicos (PSE)** ya certificado DGII (ej: TheFactoryHKA, Gosocket, ECF Express, eFactRD, ioFacturo).
- HUYGHU les delega emisión: les pasamos los datos de factura y ellos generan + firman + envían + devuelven trackID/acuse.
- **Costo**: típicamente RD$0.50–2.00 por e-CF emitido + setup RD$5,000–15,000.
- **Tiempo de integración**: 3–5 días (solo conectar API REST de PSE, sin firma propia, sin XSD propio).
- **Cumple deadline 15-mayo**: SÍ, holgadamente.
- **Pierde**: control total, dependencia de tercero, costo recurrente.
- **Gana**: certeza de cumplimiento, soporte, certificación delegada.

#### Alternativa B: Solicitar prórroga DGII

- Documentar formalmente a DGII que HUYGHU está implementando solución propia y solicitar prórroga de 60–90 días.
- DGII ha concedido prórrogas anteriormente (la actual del 15-nov-2025 → 15-may-2026 fue una prórroga).
- **Riesgo**: DGII puede negarla. Multa diaria si no cumple y no hay prórroga.

#### Alternativa C: Implementación propia con deadline real (~30-jun-2026)

- Mantener el plan de §6 pero con +6 semanas de margen.
- Permite riesgos R1-R4 absorberse.
- Requiere SÍ o SÍ Alternativa B (prórroga DGII) para no estar en multa entre 15-may y 30-jun.

### 7.3 Recomendación final

> **Combinar Alternativa A + B**:
> 1. **HOY**: integrar PSE (TheFactoryHKA recomendado por experiencia local RD) para garantizar HUYGHU emite e-CF en producción antes del 15-mayo.
> 2. **EN PARALELO**: solicitar prórroga DGII formal de 60 días "mientras se consolida solución propia".
> 3. **Q3 2026**: si Carlos quiere control total y reducir costo PSE, desarrollar implementación nativa con plan §6 ampliado a 30 días.
>
> **Por qué no implementación propia ya**: el riesgo de incumplir deadline + multar a HUYGHU + dañar reputación supera ampliamente el costo del PSE en el primer año. Además HUYGHU es **beta única**: no hay volumen que justifique el ROI de implementación propia ahora. Cuando entren más clientes (multi-tenant fase 2), entonces sí internalizar.

---

## 8. Próximos pasos accionables (HOY mismo)

1. **Carlos**: ir a OFV DGII → buscar RNC 131047939 → confirmar clasificación + fecha límite real + estado de certificado digital.
2. **Carlos**: contactar 2–3 PSE (TheFactoryHKA, ECF Express, eFactRD) para presupuesto y tiempos integración.
3. **Carlos**: si decide implementación propia, iniciar trámite certificado digital + adhesión DGII HOY.
4. **Arquitecto**: una vez Carlos decida camino → crear plan ejecución detallado wave-by-wave.
5. **NO** tocar código Go hasta tener decisión Carlos. Este documento es solo investigación.

---

## 9. Referencias verificadas

### Fuentes oficiales DGII
- [Documentación e-CF DGII (índice)](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Paginas/documentacionSobreE-CF.aspx)
- [Formato e-CF v1.0 (PDF, octubre 2025)](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Documentacin%20sobre%20eCF/Formatos%20XML/Formato%20Comprobante%20Fiscal%20Electr%C3%B3nico%20(e-CF)%20v1.0.pdf)
- [Descripción Técnica Facturación Electrónica v1.6 (PDF, junio 2023)](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Documentacin%20sobre%20eCF/Informe%20y%20Descripci%C3%B3n%20T%C3%A9cnica/Descripcion-tecnica-de-facturacion-electronica.pdf)
- [Proceso de Certificación Emisor Electrónico (PDF)](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Documentacin%20sobre%20eCF/Documentaciones%20Proceso%20de%20Certificaci%C3%B3n%20FE/Proceso%20de%20Certificacion%20para%20ser%20Emisor%20Electronico.pdf)
- [App Firma Digital DGII (PDF)](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Documentacin%20sobre%20eCF/Instructivos%20sobre%20Facturaci%C3%B3n%20Electr%C3%B3nica/Instructivo%20App%20Firma%20Digital.pdf)
- [Listados Contribuyentes Obligados DGII](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Paginas/Listados-contribuyentes-obligados-implementar-facturacion-electronica.aspx)

### Cronograma confirmado
- [Alegra blog — Obligatoriedad Facturación Electrónica RD 2025-2026](https://blog.alegra.com/republica-dominicana/obligatoriedad-de-factura-electronica/)
- [TheFactoryHKA — Ley 32-23 fechas clave](https://thefactoryhka.com.do/ley-32-23-y-la-obligatoriedad-de-factura-electronica-fechas-clave-y-todo-lo-que-debes-saber/)

### Librerías técnicas
- [victors1681/dgii-ecf — Node.js, mantenida (274 commits, 84 stars)](https://github.com/victors1681/dgii-ecf)
- [artemkunich/goxades — XAdES en Go (sin actualizaciones recientes)](https://github.com/artemkunich/goxades)
- [russellhaering/goxmldsig — XML DSig en Go (maintained)](https://github.com/russellhaering/goxmldsig)

### PSE candidatos (Alternativa A)
- [ECF Express — Proceso certificación](https://ecf.express/proceso-certificacion/)
- [TheFactoryHKA — Wiki técnico](https://felwiki.thefactoryhka.com.do/)
- [eFactRD — Ley 32-23](https://efactrd.com/ley-32-23-facturacion-electronica-rd.html)

---

## 10. Wave 4 — Research técnico profundo (300430-iter2)

> Investigación técnica complementaria a §§1–9 para evaluar **viabilidad real** de implementación nativa Go (vs PSE recomendado en §7.3) y dar plan iter2 más concreto.

### 10.1 Análisis `victors1681/dgii-ecf` (TypeScript / Node.js)

| Atributo | Hallazgo |
|---|---|
| **Repo** | https://github.com/victors1681/dgii-ecf |
| **Stars / Forks / Commits** | 84 ⭐ / 56 forks / 274 commits — **sí mantenida** |
| **Stack** | TypeScript + Node, Babel, Jest, ESLint |
| **Tipos e-CF soportados explícitos** | 31 (Crédito fiscal), 32 (Consumo) + RFCE (Resumen) y aprobaciones comerciales |
| **Tipos NO soportados aún (público)** | 33, 34, 41, 43, 44, 45, 46, 47 — el README solo cita 31 y 32 |
| **Ambientes (enum `ENVIRONMENT`)** | `TesteCF` (DEV), `CerteCF` (CERT), `eCF` (PROD) — coincide 100 % con §1.4 |
| **API pública clave** | `new ECF(certs, ENVIRONMENT.PROD)`, `ecf.authenticate()`, `ecf.sendElectronicDocument(signedXml, fileName)`, `ecf.statusTrackId(trackId)` |
| **Componentes** | `P12Reader` (lee .p12/.pfx), `Signature` (firma XML), `Transformer` (json↔xml) |
| **Librería firma interna** | NO declarada en README. Inspección típica de stack TS: probable `xml-crypto` + `xades-js` o impl. propia con `node-forge` |
| **Licencia** | No declarada en README visible — **bloqueo legal** si se quisiera fork comercial sin verificar |
| **Riesgo legal** | Sin licencia explícita = "all rights reserved" por defecto (USA copyright). Un wrapper (subprocess) **es legal**; un port directo Go **no** sin permiso autor |

**Veredicto victors1681/dgii-ecf**: usable como **referencia conceptual** y como **subproceso runtime** (Plan B §10.4), NO como base para port directo a Go por bloqueo de licencia + por solo cubrir 2 de 10 tipos e-CF.

### 10.2 Mapeo OCR `Invoice` struct → e-CF XML obligatorios

Lectura efectiva de `internal/models/invoice.go` (184 líneas, 1 archivo, NO 80 campos como suponía el plan inicial — son **~50 campos reales** entre `Invoice` + `InvoiceItem`).

#### 10.2.1 Cobertura por sección XML e-CF (basado en Formato e-CF v1.0 oct-2025)

| Sección XML e-CF | Tag obligatorio | Campo OCR `Invoice` | ¿Cubre? | Comentario |
|---|---|---|---|---|
| **IdDoc** | TipoeCF | `TipoNCF` | ⚠ parcial | OCR captura B01/B02/E31, falta mapear a 31/32 |
| IdDoc | eNCF | `NCF` | ✅ sí | NCF crudo, formato `E310000000001` para emisor |
| IdDoc | FechaVencimientoSecuencia | — | ❌ no | Dato del emisor (rango autorizado), no del OCR |
| IdDoc | IndicadorEnvioDiferido | — | ❌ no | Constante emisor, default 0 |
| IdDoc | IndicadorMontosNegociados | — | ❌ no | Constante, default 0 |
| IdDoc | TipoIngresos | — | ❌ no | Catálogo emisor: 01–06 |
| IdDoc | TipoPago | `FormaPago` | ✅ sí | OCR captura código forma pago |
| IdDoc | FechaLimitePago | `FechaVencimiento` | ✅ sí | Existe en struct |
| IdDoc | TerminoPago | — | ❌ no | Texto libre opcional |
| **Emisor** | RNCEmisor | `RNCEmisor` | ✅ sí | OCR lo extrae |
| Emisor | RazonSocialEmisor | `NombreEmisor` | ✅ sí | OCR lo extrae |
| Emisor | NombreComercial | — | ❌ no | Catálogo emisor |
| Emisor | Sucursal | — | ❌ no | Catálogo emisor |
| Emisor | DireccionEmisor | — | ❌ no | Catálogo emisor |
| Emisor | Provincia / Municipio | — | ❌ no | Catálogo emisor |
| Emisor | TablaTelefonoEmisor | — | ❌ no | Catálogo emisor |
| Emisor | CorreoEmisor | — | ❌ no | Catálogo emisor |
| Emisor | WebSite | — | ❌ no | Catálogo emisor |
| Emisor | ActividadEconomica | — | ❌ no | Catálogo emisor |
| Emisor | CodigoVendedor | — | ❌ no | Operacional emisor |
| Emisor | NumeroFacturaInterna | — | ❌ no | Operacional emisor |
| Emisor | NumeroPedidoInterno | — | ❌ no | Operacional emisor |
| Emisor | ZonaVenta | — | ❌ no | Operacional emisor |
| Emisor | RutaVenta | — | ❌ no | Operacional emisor |
| Emisor | InformacionAdicionalEmisor | — | ❌ no | Operacional emisor |
| Emisor | FechaEmision | `FechaFactura` | ✅ sí | OCR lo extrae |
| **Comprador** (31, 33, 34, 44 obligatorio; 32 opcional) | RNCComprador | `RNCReceptor` | ✅ sí | OCR lo extrae |
| Comprador | IdentificadorExtranjero | — | ❌ no | Casos exportación (46) |
| Comprador | RazonSocialComprador | `NombreReceptor` | ✅ sí | OCR lo extrae |
| Comprador | ContactoComprador | — | ❌ no | Catálogo cliente CRM |
| Comprador | CorreoComprador | — | ❌ no | Catálogo cliente CRM |
| Comprador | DireccionComprador | — | ❌ no | Catálogo cliente CRM |
| Comprador | MunicipioComprador | — | ❌ no | Catálogo cliente CRM |
| Comprador | ProvinciaComprador | — | ❌ no | Catálogo cliente CRM |
| Comprador | FechaEntrega | — | ❌ no | Operacional |
| **Totales** | MontoGravadoTotal | calcular Subtotal − Exento | ✅ derivable | OCR tiene componentes |
| Totales | MontoGravadoI1 (18 %) / I2 (16 %) / I3 (0 %) | desde `ITBISTasa` + `Subtotal` | ⚠ parcial | OCR tiene tasa única, falta multi-tasa |
| Totales | MontoExento | `ITBISExento` | ✅ sí | OCR lo extrae |
| Totales | ITBIS1 / ITBIS2 / ITBIS3 | `ITBIS` | ⚠ parcial | OCR no separa por tasa |
| Totales | TotalITBIS | `ITBIS` | ✅ sí | OCR lo extrae |
| Totales | MontoTotal | `Total` | ✅ sí | Legacy field OCR |
| Totales | MontoNoFacturable | `MontoNoFacturable` | ✅ sí | OCR lo extrae |
| **OtraMoneda** | TipoMoneda / TipoCambio | — | ❌ no | DOP por defecto, multi-moneda fase 2 |
| **DetallesItems[]** | NumeroLinea | índice array | ✅ sí | Generable |
| Item | IndicadorFacturacion | derivar ITBIS item | ⚠ parcial | OCR tiene `IsTaxed` por item |
| Item | NombreItem | `Items[].Descripcion` | ✅ sí | OCR lo extrae |
| Item | IndicadorBienoServicio | — | ❌ no | Catálogo emisor (servicios HUYGHU = 2) |
| Item | DescripcionItem | `Items[].Descripcion` | ✅ sí | OCR lo extrae |
| Item | CantidadItem | `Items[].Cantidad` | ✅ sí | OCR lo extrae |
| Item | UnidadMedida | — | ❌ no | Catálogo, default 43 (unidad) |
| Item | PrecioUnitarioItem | `Items[].PrecioUnit` | ✅ sí | OCR lo extrae |
| Item | DescuentoMonto | `Items[].Descuento` | ✅ sí | OCR lo extrae |
| Item | MontoItem | `Items[].Importe` | ✅ sí | OCR lo extrae |
| **Subtotales informativos** | varios | — | ❌ no | Calculables al armar XML |
| **DescuentosORecargos** | TipoAjuste / Monto | `Descuento` (global) | ⚠ parcial | OCR solo descuento global, no recargos |

#### 10.2.2 Resumen cobertura

| Métrica | Valor |
|---|---|
| Tags XML e-CF totales (estimado v1.0 oct-2025) | ~120 (incluyendo opcionales) |
| Tags **obligatorios para tipo 31** (mínimo) | ~35 |
| Tags **obligatorios para tipo 32** (mínimo) | ~25 |
| Cobertura OCR sobre obligatorios tipo 31 | **~17/35 = 49 %** |
| Cobertura OCR sobre obligatorios tipo 32 | **~15/25 = 60 %** |
| Gap principal | **Datos catálogo emisor** (dirección, teléfono, actividad económica, etc) y **multi-tasa ITBIS** |

> **Implicación**: el OCR cubre la mitad de lo necesario para emitir e-CF. La otra mitad debe venir de **catálogos pre-cargados de la empresa emisora** (HUYGHU SRL en `empresas` table) + **catálogos cliente** (en `clientes` table). Esto requiere:
> - Migración BD añadiendo `empresas.ecf_emisor_config` (jsonb con todos los datos catálogo)
> - Migración BD añadiendo `clientes.ecf_comprador_data` (jsonb con dirección, contacto, correo)
> - Validador previo a emisión que verifique que ambos catálogos están completos

### 10.3 Tipos e-CF aplicables a HUYGHU SRL

HUYGHU SRL es **despacho contable** (servicios profesionales B2B principalmente, B2C ocasional).

| Código | Nombre | ¿HUYGHU emite? | Prioridad |
|---|---|---|---|
| **31** | Crédito Fiscal | ✅ SÍ — facturación a empresas clientes contribuyentes | **P0 (crítico)** |
| **32** | Consumo | ✅ SÍ — facturación a personas físicas / clientes finales | **P0 (crítico)** |
| **33** | Nota Débito | ✅ Ocasional — ajustes a alzas de facturas previas | **P1 (alto)** |
| **34** | Nota Crédito | ✅ Frecuente — anulaciones, devoluciones, descuentos | **P0 (crítico)** |
| **41** | Compras (proveedor informal) | ❌ NO emite. Sería emisor solo si compra a informal y debe declararlo. Caso raro despacho contable | P3 (baja) |
| **43** | Gastos Menores | ❌ NO suele aplicar a despacho contable | P3 (baja) |
| **44** | Regímenes Especiales | ❌ NO aplica (HUYGHU régimen ordinario) | — |
| **45** | Gubernamental | ❌ NO emite (es para entes públicos emisores) | — |
| **46** | Exportaciones | ❌ NO aplica (servicios solo locales RD) | — |
| **47** | Pagos Exterior | ❌ NO aplica | — |

> **Conclusión scope MVP HUYGHU**: tipos **31, 32, 34** son obligatorios para go-live. Tipo **33** puede esperar fase 2. Resto NO se implementa.

### 10.4 Decisión técnica firma XAdES — actualización

#### Opciones evaluadas

| Opción | Lib | Pros | Contras | Veredicto |
|---|---|---|---|---|
| **A** Go nativo `goxades` | github.com/artemkunich/goxades | Soporta SHA-256 + C14N exclusiva confirmado. Combina `beevik/etree` + `russellhaering/goxmldsig` | **10 ⭐ solo. 11 commits totales. 3 issues abiertos. Mantenimiento incierto.** Sin Go-version mínima declarada | **Riesgo medio-alto** |
| **B** Go fork `goxades_sri` (Ecuador SRI) | github.com/digitalautonomy/goxades_sri | Fork mantenido con extras. 18 commits | Adaptado a SRI Ecuador, no RD. "ID con números aleatorios" SRI no compatible con DGII RD | **Descartado** (necesita re-port) |
| **C** Combinar primitivos Go | `russellhaering/goxmldsig` (XMLDSig base, mantenida) + impl propia XAdES wrapper | Lib base mantenida, control total | **Implementar XAdES manualmente** = ~500 LOC + tests + validación contra DGII | **Riesgo alto, esfuerzo 5–7 días** |
| **D** Wrapper Node.js `victors1681/dgii-ecf` via subprocess | Spawn `node ./signer.js` desde Go | Reutiliza lib mantenida y validada DGII. Lic legal (subproceso, no port) | Requiere instalar Node 18+ en container Go. Latencia 100–300 ms por firma. Container más pesado (+150 MB) | **Plan B viable** |
| **E** Cgo OpenSSL nativo | OpenSSL bindings | Producción comprobada en otros lenguajes | Cgo en Docker scratch = pesadilla. Cross-compile imposible | **Descartado** |

#### Recomendación firma actualizada

> **Plan A** (intentar `goxades` puro Go) con **Plan B (Node subprocess) como fallback** si tras 2 días no se logra firma válida en TesteCF DGII.
>
> Justificación: `goxades` cumple los 3 requisitos técnicos (XAdES-BES + SHA-256 + C14N exclusiva), pero su falta de adopción (10⭐) y de tests reales contra DGII RD significa que puede fallar en detalles de canonicalización de namespaces o atributos `Id` de `<Reference>`. Si el camino A falla rápido, el camino B funciona seguro porque `victors1681/dgii-ecf` ya está validado contra DGII.

### 10.5 Endpoints DGII — confirmación

| Ambiente | Recepción e-CF | Auth (seed) | Consulta estado |
|---|---|---|---|
| **TesteCF** (DEV/sandbox) | `https://ecf.dgii.gov.do/testecf/recepcion/api/facturaselectronicas` | `https://ecf.dgii.gov.do/testecf/autenticacion/api/autenticacion/semilla` | `https://ecf.dgii.gov.do/testecf/consultaresultado/api/consultas/estado?trackid={id}` |
| **CerteCF** (certificación pre-prod) | `https://ecf.dgii.gov.do/certecf/recepcion/api/facturaselectronicas` | `https://ecf.dgii.gov.do/certecf/autenticacion/api/autenticacion/semilla` | `https://ecf.dgii.gov.do/certecf/consultaresultado/api/consultas/estado?trackid={id}` |
| **eCF** (producción) | `https://ecf.dgii.gov.do/ecf/recepcion/api/facturaselectronicas` | `https://ecf.dgii.gov.do/ecf/autenticacion/api/autenticacion/semilla` | `https://ecf.dgii.gov.do/ecf/consultaresultado/api/consultas/estado?trackid={id}` |

#### Flujo autenticación (seed → JWT)

```
1. GET  {host}/autenticacion/api/autenticacion/semilla
   → Response: XML con elemento <semilla>VALOR_RANDOM</semilla> y fecha
2. Cliente firma el XML semilla con cert digital privado (XAdES-BES)
3. POST {host}/autenticacion/api/autenticacion/validacioncertificado
   Body: XML semilla firmado (multipart/form-data, field name "xml")
   → Response JSON: { "token": "eyJhbGc...", "expira": "ISO_DATE", "expedido": "ISO_DATE" }
4. Header en todos calls posteriores: Authorization: Bearer <token>
   Token TTL típico: 1 hora
```

#### Estados respuesta DGII

| Estado | Significado | Acción cliente |
|---|---|---|
| `Aceptado` | e-CF válido, registrado | terminal — guardar trackID + fecha respuesta |
| `Rechazado` | e-CF inválido (errores estructurales o validación) | terminal — corregir y re-emitir con NUEVO eNCF |
| `Aceptado Condicional` | Aceptado con observaciones (warnings no bloqueantes) | terminal — registrar + flag para revisión |
| `EnProceso` | Aún en validación DGII | seguir polling cada 30 s |

#### SLA DGII (no oficial, observado por integradores)

- Respuesta inicial al POST recepción: **<5 segundos típico, hasta 30 s en horas pico**.
- Acuse final disponible: **30 s a 5 min** típico, hasta 24 h en casos extremos.
- Recomendado: polling exponential backoff (5 s, 15 s, 60 s, 300 s) durante 24 h max.

### 10.6 Plan iteración 2 — más concreto

> Asume Carlos confirma camino implementación propia (NO PSE). Si elige PSE, este plan se descarta.

#### Wave 0 — Bloqueantes humanos (Carlos, paralelo a todo)

- Verificar clasificación HUYGHU en OFV (RNC 131047939) → confirmar deadline real
- Tramitar cert digital si no existe
- Iniciar adhesión DGII formal (web OFV → solicitud emisor electrónico)

#### Wave 1 — Schema BD + catálogos (Día 1, sin código firma)

```sql
-- Migración 30-Abr
ALTER TABLE empresas ADD COLUMN ecf_emisor_config jsonb DEFAULT '{}'::jsonb;
-- Estructura ecf_emisor_config: { rnc, razon_social, nombre_comercial, sucursal, direccion, provincia,
--   municipio, telefono[], correo, website, actividad_economica, fecha_vencimiento_secuencia,
--   secuencia_actual: { "31": 1, "32": 1, "34": 1 }, ... }

ALTER TABLE clientes ADD COLUMN ecf_comprador_data jsonb DEFAULT '{}'::jsonb;
-- Estructura: { contacto, correo, direccion, municipio, provincia }

CREATE TABLE ecf_certificados_emisor (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id uuid REFERENCES empresas(id),
  cert_p12_path text NOT NULL,             -- ruta MinIO al .p12
  cert_password_encrypted text NOT NULL,   -- AES-256 con KMS
  cert_subject_dn text,
  cert_serial text,
  cert_issuer text,
  expires_at timestamptz NOT NULL,
  is_active boolean DEFAULT true,
  created_at timestamptz DEFAULT now()
);

CREATE TABLE ecf_outbox (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  empresa_id uuid REFERENCES empresas(id),
  factura_id uuid REFERENCES facturas_clientes(id),
  tipo_ecf char(2) NOT NULL,               -- '31', '32', '34'
  encf text NOT NULL UNIQUE,                -- ej: 'E310000000001'
  xml_unsigned text NOT NULL,
  xml_signed text,
  ambiente text NOT NULL,                   -- 'TESTECF', 'CERTECF', 'ECF'
  estado_local text NOT NULL DEFAULT 'borrador',  -- borrador, firmado, enviado, aceptado, rechazado, aceptado_condicional
  trackid text,
  enviado_at timestamptz,
  respuesta_inicial jsonb,
  created_at timestamptz DEFAULT now(),
  updated_at timestamptz DEFAULT now()
);

CREATE TABLE ecf_acuses_dgii (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  outbox_id uuid REFERENCES ecf_outbox(id),
  trackid text NOT NULL,
  estado text NOT NULL,                     -- 'Aceptado', 'Rechazado', 'Aceptado Condicional', 'EnProceso'
  fecha_recepcion timestamptz,
  mensaje text,
  payload_completo jsonb,
  consultado_at timestamptz DEFAULT now()
);

CREATE INDEX idx_ecf_outbox_estado ON ecf_outbox(estado_local) WHERE estado_local IN ('enviado', 'aceptado_condicional');
CREATE INDEX idx_ecf_acuses_trackid ON ecf_acuses_dgii(trackid);
```

#### Wave 2 — Templates XML Go (Día 2-3, paralelo)

- 3 archivos `internal/ecf/templates/{tipo31,tipo32,tipo34}.go` — structs serializables vía `encoding/xml`
- Generador `internal/ecf/builder.go` — `BuildECFXML(emisorConfig, compradorData, factura) → ([]byte, error)`
- Tests `internal/ecf/builder_test.go` — validar contra XSD oficial DGII (parsearlos primero a Go XSD validator usando `github.com/lestrrat-go/libxml2` o invocar `xmllint` subprocess)

#### Wave 3 — Firma XAdES (Día 4-5, secuencial)

- Día 4: prototipo con `goxades` puro. Si no produce firma válida en 1 día → switch a Plan B
- Día 5: Plan B fallback — `internal/ecf/signer/node_signer.go` invoca `node /opt/dgii-signer/sign.js` con stdin XML + return stdout XML firmado. Container Dockerfile suma `apk add nodejs npm && npm i dgii-ecf`

#### Wave 4 — Cliente HTTP DGII (Día 6, en paralelo Wave 3)

- `internal/ecf/dgii/client.go` — método `Authenticate(ambiente)` (GET seed → firma → POST → JWT), `SendECF(xmlSigned, fileName)`, `ConsultaEstado(trackid)`
- Cache JWT con TTL 50 min (refresh antes de expirar)

#### Wave 5 — Endpoint emisión + worker polling (Día 7-8)

- `POST /api/ecf/emitir` (request: factura_id) → orchestrate build → sign → send → save outbox
- Worker async cada 30 s busca `ecf_outbox` con estado `enviado` y polls `consultaEstado`

#### Wave 6 — Pruebas TesteCF (Día 9-10)

- Suite de pruebas E2E contra `https://ecf.dgii.gov.do/testecf/...`
- Validar al menos 1 de cada tipo: 31, 32, 34 — aceptado por DGII

#### Wave 7 — Certificación CerteCF (Día 11-15)

- DGII exige batch de pruebas formales (típicamente 5 a 10 escenarios por tipo). Iterar hasta DGII apruebe.
- **Sin SLA DGII**: este wave puede tomar de 3 días a 3 semanas dependiendo de carga DGII.

#### Wave 8 — Producción (cuando DGII apruebe formalmente)

- Switch ambiente `eCF` → emisión real

### 10.7 Comparación Wave 4 iter1 vs iter2

| Aspecto | iter1 (sin research profundo) | iter2 (este doc) |
|---|---|---|
| Cobertura OCR → e-CF | "OCR cubre 80 campos" (falso, son ~50 reales) | "OCR cubre ~50 % de obligatorios; 50 % restante de catálogos pre-cargados" |
| Lib firma | "Usar goxades + goxmldsig" (sin verificar mantenimiento) | "goxades intentar Plan A; Plan B Node subprocess; ambos con justificación" |
| Tipos e-CF MVP | "31, 32, 33, 34" (los 4) | "31, 32, 34 P0; 33 P1 fase 2" |
| Estimación esfuerzo | "15 días-dev" | "15 días-dev confirmado, pero con Wave 0 humana en paralelo y Wave 7 sin SLA" |
| Recomendación final | "Combinar PSE (A) + prórroga (B)" | **CONFIRMADA** sin cambios. Iter2 refuerza por qué. |

### 10.8 Recomendación final §11 actualizada

> **No cambia respecto a §7.3**: PSE (Alternativa A) + Prórroga (Alternativa B) sigue siendo la **decisión correcta** dado:
>
> 1. Cobertura OCR insuficiente (49 % campos obligatorios tipo 31) → necesita poblar catálogos extras antes de poder emitir
> 2. Lib firma Go nativa (`goxades`) tiene 10 ⭐ y baja adopción → no validada contra DGII RD
> 3. Plan B (Node subprocess `victors1681/dgii-ecf`) es funcional pero añade complejidad runtime (Node + Go en mismo container)
> 4. Wave 7 (certificación DGII) sin SLA → riesgo schedule fuera de control
> 5. **HUYGHU es beta única** (1 cliente) → ROI implementación propia es negativo en año 1
>
> **Refinamiento iter2**: si Carlos insiste en implementación propia tras leer esto:
> - Adoptar Plan B (Node subprocess) desde día 1 — saltar intento `goxades` puro Go
> - Reducir scope MVP a tipos **31 y 34 solo** (deferir 32 — HUYGHU es 90 % B2B contribuyentes)
> - Planificar 30 días en lugar de 15 (absorber Wave 7 sin SLA)
> - Solicitar prórroga DGII formal de 60 días en paralelo (cubre slip)

### 10.9 Referencias adicionales (iter2)

- [victors1681/dgii-ecf README — Node.js lib mantenida](https://github.com/victors1681/dgii-ecf)
- [artemkunich/goxades — XAdES Go (10 ⭐, mantenimiento incierto)](https://github.com/artemkunich/goxades)
- [russellhaering/goxmldsig — XML DSig Go (mantenida, base de goxades)](https://github.com/russellhaering/goxmldsig)
- [digitalautonomy/goxades_sri — Fork goxades para Ecuador SRI](https://github.com/digitalautonomy/goxades_sri)
- [SSD-Smart-Software-Development-SRL/ecf_dgii — .NET Core impl referencia](https://github.com/SSD-Smart-Software-Development-SRL/ecf_dgii)
- [DGII — Listados Contribuyentes Obligados](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Paginas/Listados-contribuyentes-obligados-implementar-facturacion-electronica.aspx)
- [Alanube blog — Cronograma 2025-2026 PYMES](https://blog.alegra.com/republica-dominicana/obligatoriedad-de-factura-electronica/)
- [Gosocket — Actualizaciones XSD oct-2025 (10 esquemas modificados)](https://gosocket.net/centro-de-recursos/la-dgii-actualiza-diez-esquemas-de-xsd-e-cf-v-1-0-octubre-2025/)

---

**Última actualización**: 2026-04-28 por arquitecto FacturaIA — Wave 4 iter2 (research técnico profundo). Sin código Go modificado. Sin commits.
