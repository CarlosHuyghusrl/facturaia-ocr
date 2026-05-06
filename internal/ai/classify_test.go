package ai

import (
	"testing"

	"github.com/facturaIA/invoice-ocr-service/internal/models"
)

func TestClassifyTipoFactura_EmisorEqualsEmpresa(t *testing.T) {
	invoice := &models.Invoice{RNCEmisor: "131047939"}
	empresaRNC := "131047939"
	ClassifyTipoFactura(invoice, empresaRNC)
	if invoice.TipoFactura != "ventas" {
		t.Errorf("expected 'ventas', got '%s'", invoice.TipoFactura)
	}
}

func TestClassifyTipoFactura_EmisorDistintoEmpresa(t *testing.T) {
	invoice := &models.Invoice{RNCEmisor: "101019921"}
	empresaRNC := "131047939"
	ClassifyTipoFactura(invoice, empresaRNC)
	if invoice.TipoFactura != "gastos" {
		t.Errorf("expected 'gastos', got '%s'", invoice.TipoFactura)
	}
}

func TestClassifyTipoFactura_EmisorVacio(t *testing.T) {
	// Cuando emisor_rnc está vacío, no hay override — el TipoFactura queda como estaba
	invoice := &models.Invoice{RNCEmisor: "", TipoFactura: "gastos"}
	empresaRNC := "131047939"
	ClassifyTipoFactura(invoice, empresaRNC)
	// No debe haber override — conserva "gastos" (lo que Gemini devolvió)
	if invoice.TipoFactura != "gastos" {
		t.Errorf("expected TipoFactura unchanged ('gastos'), got '%s'", invoice.TipoFactura)
	}
}

func TestClassifyTipoFactura_EmisorEqualsEmpresa_ConGuiones(t *testing.T) {
	// RNC con guiones debe normalizarse correctamente
	invoice := &models.Invoice{RNCEmisor: "1-31-04793-9"}
	empresaRNC := "131047939"
	ClassifyTipoFactura(invoice, empresaRNC)
	if invoice.TipoFactura != "ventas" {
		t.Errorf("expected 'ventas' with dashes normalized, got '%s'", invoice.TipoFactura)
	}
}
