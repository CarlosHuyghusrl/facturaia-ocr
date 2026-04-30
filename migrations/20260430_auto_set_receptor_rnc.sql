-- Migration: auto-set receptor_rnc desde clientes/empresas si OCR no lo extrae
-- Aplica a INSERT y UPDATE de cliente_id/empresa_id/receptor_rnc
-- Fecha: 2026-04-30
-- Razon: Cuando el handler OCR procesa una factura nueva sin receptor_rnc,
--        derivarlo automaticamente del cliente o empresa asociada.

CREATE OR REPLACE FUNCTION auto_set_receptor_rnc()
RETURNS TRIGGER AS $$
DECLARE
  v_rnc TEXT;
BEGIN
  IF NEW.receptor_rnc IS NULL OR NEW.receptor_rnc = '' THEN
    -- Try cliente lookup first
    IF NEW.cliente_id IS NOT NULL THEN
      SELECT REPLACE(COALESCE(rnc_cedula, ''), '-', '')
        INTO v_rnc
        FROM clientes
        WHERE id = NEW.cliente_id;
      IF v_rnc IS NOT NULL AND v_rnc <> '' THEN
        NEW.receptor_rnc := v_rnc;
      END IF;
    END IF;

    -- Fallback: empresa
    IF (NEW.receptor_rnc IS NULL OR NEW.receptor_rnc = '') AND NEW.empresa_id IS NOT NULL THEN
      SELECT REPLACE(COALESCE(rnc, ''), '-', '')
        INTO v_rnc
        FROM empresas
        WHERE id = NEW.empresa_id;
      IF v_rnc IS NOT NULL AND v_rnc <> '' THEN
        NEW.receptor_rnc := v_rnc;
      END IF;
    END IF;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_auto_set_receptor_rnc ON facturas_clientes;
CREATE TRIGGER trg_auto_set_receptor_rnc
  BEFORE INSERT OR UPDATE OF cliente_id, empresa_id, receptor_rnc ON facturas_clientes
  FOR EACH ROW EXECUTE FUNCTION auto_set_receptor_rnc();

-- Verificar
DO $$
DECLARE
  v_count INT;
BEGIN
  SELECT COUNT(*) INTO v_count FROM pg_trigger WHERE tgname = 'trg_auto_set_receptor_rnc';
  RAISE NOTICE 'Trigger trg_auto_set_receptor_rnc creado: % filas', v_count;
END $$;
