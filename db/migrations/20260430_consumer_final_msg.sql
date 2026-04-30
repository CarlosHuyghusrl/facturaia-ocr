-- Migration: 20260430_consumer_final_msg
-- Hito: trigger-606-consumer-final-msg
-- Fix: persistir consumer_final_b02 message en review_notes para B02/E32
-- aunque required_ok=true. Idempotente (filtra BD-managed codes antes de append).

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
  v_existing_errors JSONB := '[]'::jsonb;
  v_preserved_errors JSONB := '[]'::jsonb;
  v_final_errors JSONB := '[]'::jsonb;
  v_managed_codes TEXT[] := ARRAY['missing_emisor_rnc','invalid_ncf','missing_date','invalid_amount','consumer_final_b02'];
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

  -- 3. STRICT REQUIRED FIELDS GATE: solo si era candidata 606
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

  -- 4. Mensaje informativo consumidor final (B02/E32)
  IF NEW.ncf IS NOT NULL AND length(NEW.ncf) >= 3 AND upper(left(NEW.ncf, 3)) IN ('B02','E32') THEN
    v_errors := v_errors || jsonb_build_object('field','ncf','code','consumer_final_b02','message','NCF B02/E32 = consumidor final. Es gasto pero NO va al 606.');
  END IF;

  NEW.aplica_606 := v_aplica;

  -- 5. Status review solo si falló required (consumer_final solo es info, sigue 'validated')
  IF NOT v_required_ok THEN
    NEW.extraction_status := 'review';
  END IF;

  -- 6. Persistir review_notes SIEMPRE que v_errors tenga contenido (idempotente)
  --    Filtra BD-managed codes existentes para evitar duplicados en re-triggers
  --    Preserva codes del validator Go (itbis_mismatch, total_mismatch, nota_credito_sin_referencia, etc.)
  IF jsonb_array_length(v_errors) > 0 THEN
    BEGIN
      IF NEW.review_notes IS NOT NULL AND NEW.review_notes <> '' THEN
        v_existing_errors := COALESCE(NEW.review_notes::jsonb->'errors', '[]'::jsonb);
        SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb) INTO v_preserved_errors
          FROM jsonb_array_elements(v_existing_errors) elem
          WHERE NOT (elem->>'code' = ANY(v_managed_codes));
      ELSE
        v_preserved_errors := '[]'::jsonb;
      END IF;
      v_final_errors := v_preserved_errors || v_errors;
      NEW.review_notes := jsonb_build_object(
        'valid', v_required_ok,
        'errors', v_final_errors,
        'warnings', COALESCE(
          CASE WHEN NEW.review_notes IS NOT NULL AND NEW.review_notes <> ''
               THEN NEW.review_notes::jsonb->'warnings' END,
          '[]'::jsonb
        )
      )::text;
    EXCEPTION WHEN OTHERS THEN
      NEW.review_notes := jsonb_build_object('valid', v_required_ok, 'errors', v_errors, 'warnings', '[]'::jsonb)::text;
    END;
  END IF;

  -- 7. Periodo + itbis_adelantar
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

-- Forzar re-evaluación: B02/E32 + aplica_606=true con campos vacíos (idempotente)
UPDATE facturas_clientes
SET monto = monto
WHERE (
  (ncf IS NOT NULL AND (upper(left(ncf,3)) = 'B02' OR upper(left(ncf,3)) = 'E32'))
  OR (aplica_606 = true AND (emisor_rnc IS NULL OR emisor_rnc = '' OR ncf IS NULL OR length(ncf) < 11 OR fecha_documento IS NULL OR monto <= 0))
);
