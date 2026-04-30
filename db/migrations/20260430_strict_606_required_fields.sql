-- Migration: 20260430_strict_606_required_fields
-- Hito: validacion-pre-606-app
-- Modifica auto_tag_factura_606 para validar campos requeridos antes de aplica_606=true

CREATE OR REPLACE FUNCTION public.auto_tag_factura_606()
RETURNS trigger LANGUAGE plpgsql AS $func$
DECLARE
  v_prefijo VARCHAR(3);
  v_aplica  BOOLEAN := false;
  v_cliente_rnc TEXT;
  v_emisor_norm TEXT;
  v_errors JSONB := '[]'::jsonb;
  v_required_ok BOOLEAN := true;
  v_was_candidate BOOLEAN := false;
BEGIN
  -- 1. Clasificación base por prefijo NCF
  IF NEW.ncf IS NOT NULL AND length(NEW.ncf) >= 3 THEN
    v_prefijo := upper(left(NEW.ncf, 3));
    SELECT aplica_606 INTO v_aplica FROM dgii_ncf_tipos WHERE prefijo = v_prefijo;
    IF v_aplica IS NULL THEN v_aplica := false; END IF;
  END IF;

  v_was_candidate := v_aplica;

  -- 2. Override por perspectiva: si emisor_rnc = rnc del cliente, ES venta
  IF NEW.cliente_id IS NOT NULL AND NEW.emisor_rnc IS NOT NULL AND NEW.emisor_rnc <> '' THEN
    SELECT REPLACE(COALESCE(rnc_cedula,''),'-','') INTO v_cliente_rnc FROM clientes WHERE id = NEW.cliente_id;
    v_emisor_norm := REPLACE(COALESCE(NEW.emisor_rnc,''),'-','');
    IF v_cliente_rnc IS NOT NULL AND v_cliente_rnc <> '' AND v_cliente_rnc = v_emisor_norm THEN
      v_aplica := false;
    END IF;
  END IF;

  -- 3. STRICT REQUIRED FIELDS GATE: solo si era candidata 606 verificar required
  IF v_was_candidate AND v_aplica THEN
    IF NEW.emisor_rnc IS NULL OR NEW.emisor_rnc = '' THEN
      v_aplica := false;
      v_errors := v_errors || jsonb_build_object('field','emisor_rnc','code','missing_emisor_rnc','message','Falta RNC del emisor. Edita el campo y guarda para que aplique al 606.');
      v_required_ok := false;
    END IF;
    IF NEW.ncf IS NULL OR length(NEW.ncf) < 11 THEN
      v_aplica := false;
      v_errors := v_errors || jsonb_build_object('field','ncf','code','invalid_ncf','message','NCF inválido (formato esperado: B01XXXXXXXXX)');
      v_required_ok := false;
    END IF;
    IF NEW.fecha_documento IS NULL THEN
      v_aplica := false;
      v_errors := v_errors || jsonb_build_object('field','fecha_documento','code','missing_date','message','Falta fecha del documento');
      v_required_ok := false;
    END IF;
    IF COALESCE(NEW.monto, 0) <= 0 THEN
      v_aplica := false;
      v_errors := v_errors || jsonb_build_object('field','monto','code','invalid_amount','message','Monto inválido (debe ser > 0)');
      v_required_ok := false;
    END IF;
  END IF;

  -- 4. Marcar consumidor final para mensajes informativos
  IF NEW.ncf IS NOT NULL AND length(NEW.ncf) >= 3 AND upper(left(NEW.ncf, 3)) IN ('B02','E32') THEN
    v_errors := v_errors || jsonb_build_object('field','ncf','code','consumer_final_b02','message','NCF B02/E32 = consumidor final. Es gasto pero NO va al 606.');
  END IF;

  NEW.aplica_606 := v_aplica;

  -- 5. Si era candidata 606 pero falló required, marcar review
  IF NOT v_required_ok THEN
    NEW.extraction_status := 'review';
    BEGIN
      IF NEW.review_notes IS NULL OR NEW.review_notes = '' THEN
        NEW.review_notes := jsonb_build_object('valid', false, 'errors', v_errors, 'warnings', '[]'::jsonb)::text;
      ELSE
        NEW.review_notes := jsonb_set(
          NEW.review_notes::jsonb,
          '{errors}',
          COALESCE(NEW.review_notes::jsonb->'errors', '[]'::jsonb) || v_errors
        )::text;
      END IF;
    EXCEPTION WHEN OTHERS THEN
      NEW.review_notes := jsonb_build_object('valid', false, 'errors', v_errors, 'warnings', '[]'::jsonb)::text;
    END;
  END IF;

  -- 6. Periodo + itbis_adelantar
  IF NEW.fecha_documento IS NOT NULL THEN
    NEW.periodo_606 := to_char(NEW.fecha_documento, 'YYYYMM');
  END IF;

  IF NEW.itbis_adelantar IS NULL OR NEW.itbis_adelantar = 0 THEN
    NEW.itbis_adelantar := GREATEST(0,
      COALESCE(NEW.itbis,0) - COALESCE(NEW.itbis_retenido,0)
      - COALESCE(NEW.itbis_proporcionalidad,0) - COALESCE(NEW.itbis_costo,0));
  END IF;

  RETURN NEW;
END;
$func$;

-- Forzar re-evaluación de filas existentes (UPDATE no-op para disparar trigger BEFORE UPDATE)
UPDATE facturas_clientes
SET monto = monto
WHERE aplica_606 = true
  AND (emisor_rnc IS NULL OR emisor_rnc = '' OR ncf IS NULL OR length(ncf) < 11 OR fecha_documento IS NULL OR monto <= 0);
