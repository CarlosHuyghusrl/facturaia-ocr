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

**Última actualización**: 2026-04-28 por arquitecto FacturaIA. Sin código Go modificado. Sin commits.
