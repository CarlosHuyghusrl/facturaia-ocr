-- Migration: 20260501_invoice_review_fix
-- Hito: facturaia-bugs-p0-invoice-review-w2
--
-- PROBLEMA: índice UNIQUE (ncf, empresa_id) sobre facturas_clientes bloquea
-- múltiples filas con ncf='' o ncf=NULL para la misma empresa. Caso real:
-- CENTRAL LINK TV (OCR no detectó NCF) no pudo backfillearse a empresa_id=Huyghu
-- por choque con qb-sync row también con ncf vacío.
--
-- FIX: convertir índice a UNIQUE PARCIAL excluyendo ncf NULL/empty.
-- Las facturas con NCF válido siguen sujetas a unique (no duplicados);
-- las facturas sin NCF (consumidor final B02 o OCR fail) coexisten múltiples
-- por empresa hasta que el usuario edite el NCF.
--
-- TAMBIÉN: trigger BEFORE UPDATE para touch updated_at en cada UPDATE.
-- Si la columna no existe, el trigger es no-op (skip).

BEGIN;

-- 1. DROP índice antiguo (si existe)
DROP INDEX IF EXISTS public.idx_facturas_clientes_ncf_empresa;

-- 2. CREATE índice parcial: solo aplica cuando NCF está poblado
CREATE UNIQUE INDEX idx_facturas_clientes_ncf_empresa
  ON public.facturas_clientes (ncf, empresa_id)
  WHERE ncf IS NOT NULL AND ncf <> '';

-- 3. Función + trigger updated_at (idempotente)
CREATE OR REPLACE FUNCTION public.update_modified_column()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND NEW IS DISTINCT FROM OLD THEN
    -- Solo actualizar updated_at si el row realmente cambió
    BEGIN
      NEW.updated_at := now();
    EXCEPTION WHEN undefined_column THEN
      -- updated_at no existe en esta tabla: no-op
      NULL;
    END;
  END IF;
  RETURN NEW;
END;
$$;

-- 4. Drop + create trigger sobre facturas_clientes
DROP TRIGGER IF EXISTS trg_update_facturas_clientes_modified ON public.facturas_clientes;

-- Solo crear trigger si la columna updated_at existe
DO $$
DECLARE
  has_col BOOLEAN;
BEGIN
  SELECT EXISTS(
    SELECT 1 FROM information_schema.columns
    WHERE table_schema='public' AND table_name='facturas_clientes' AND column_name='updated_at'
  ) INTO has_col;

  IF has_col THEN
    EXECUTE 'CREATE TRIGGER trg_update_facturas_clientes_modified
             BEFORE UPDATE ON public.facturas_clientes
             FOR EACH ROW EXECUTE FUNCTION public.update_modified_column()';
  END IF;
END $$;

-- 5. Verificación post-apply
SELECT 'Index recreated as partial' AS status,
       indexdef
FROM pg_indexes
WHERE tablename='facturas_clientes' AND indexname='idx_facturas_clientes_ncf_empresa';

COMMIT;
