-- ============================================================
-- Migration: 20260428_add_aplica_608.sql
-- Propósito : Soporte Formato 608 DGII (NCF Anulados)
-- Fuente    : Norma General 06-2018, instructivo §12 dgii-fiscal skill
-- Endpoint  : GET /api/formato-608/{rnc}?periodo=YYYYMM
-- NO ejecutar automáticamente — coordinar con Carlos / GestoriaRD deploy.
-- Idempotente: usa IF NOT EXISTS / OR REPLACE.
-- ============================================================

-- ──────────────────────────────────────────────────────────────
-- PARTE 1: Columnas en facturas_clientes
-- ──────────────────────────────────────────────────────────────
ALTER TABLE facturas_clientes
  ADD COLUMN IF NOT EXISTS aplica_608      BOOLEAN     DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS periodo_608     VARCHAR(6),
  ADD COLUMN IF NOT EXISTS fecha_anulacion DATE,
  ADD COLUMN IF NOT EXISTS tipo_anulacion  VARCHAR(2);

COMMENT ON COLUMN facturas_clientes.aplica_608      IS 'TRUE si NCF aplica Formato 608 (anulado). Auto-tag por trigger.';
COMMENT ON COLUMN facturas_clientes.periodo_608     IS 'YYYYMM derivado de fecha_anulacion (o fecha_documento). Auto-calculado.';
COMMENT ON COLUMN facturas_clientes.fecha_anulacion IS 'Fecha en que el contribuyente anuló el NCF.';
COMMENT ON COLUMN facturas_clientes.tipo_anulacion  IS 'Código DGII tipo anulación: 01-deterioro, 02-error impresión, 03-impresión defectuosa, 04-duplicidad, 05-corrección info, 06-cese operaciones, 07-pérdida/hurto, 08-NCF inutilizado, 09-otros.';

-- ──────────────────────────────────────────────────────────────
-- PARTE 2: Índice de consulta por (empresa, periodo) para 608
-- ──────────────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_facturas_clientes_608
  ON facturas_clientes (empresa_id, periodo_608)
  WHERE aplica_608 = TRUE;

CREATE INDEX IF NOT EXISTS idx_facturas_clientes_periodo_608
  ON facturas_clientes (periodo_608);

-- ──────────────────────────────────────────────────────────────
-- PARTE 3: Trigger auto_tag_factura_608
--   - Se dispara BEFORE INSERT/UPDATE.
--   - NCF B14 (régimenes especiales / nota crédito) o estado='anulada'
--     o presencia de fecha_anulacion → marca aplica_608 = TRUE.
--   - periodo_608 = YYYYMM de fecha_anulacion (fallback fecha_documento).
--   - NO toca aplica_606 (independiente — NO modificar trigger 606).
-- ──────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION auto_tag_factura_608()
RETURNS TRIGGER AS $$
BEGIN
  IF NEW.ncf IS NOT NULL AND (
       NEW.ncf ~ '^B14'
       OR NEW.estado = 'anulada'
       OR NEW.fecha_anulacion IS NOT NULL
     ) THEN
    NEW.aplica_608 := TRUE;
    NEW.periodo_608 := COALESCE(
      to_char(NEW.fecha_anulacion, 'YYYYMM'),
      to_char(NEW.fecha_documento, 'YYYYMM')
    );
  ELSE
    -- mantener default si nadie lo marcó manualmente
    IF NEW.aplica_608 IS NULL THEN
      NEW.aplica_608 := FALSE;
    END IF;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_auto_tag_608 ON facturas_clientes;
CREATE TRIGGER trg_auto_tag_608
  BEFORE INSERT OR UPDATE ON facturas_clientes
  FOR EACH ROW EXECUTE FUNCTION auto_tag_factura_608();

-- ──────────────────────────────────────────────────────────────
-- PARTE 4: Backfill — re-evalúa filas existentes sin tocar 606
-- ──────────────────────────────────────────────────────────────
-- El UPDATE dispara el trigger sin cambiar valores reales.
UPDATE facturas_clientes
SET fecha_documento = fecha_documento
WHERE ncf IS NOT NULL
  AND (estado = 'anulada' OR ncf ~ '^B14' OR fecha_anulacion IS NOT NULL);

-- ──────────────────────────────────────────────────────────────
-- PARTE 5: Verificación
-- ──────────────────────────────────────────────────────────────
DO $$
BEGIN
  RAISE NOTICE '=== MIGRATION 608 OK ===';
  RAISE NOTICE 'aplica_608=true  : %', (SELECT COUNT(*) FROM facturas_clientes WHERE aplica_608 = TRUE);
  RAISE NOTICE 'aplica_608=false : %', (SELECT COUNT(*) FROM facturas_clientes WHERE aplica_608 = FALSE);
END $$;
