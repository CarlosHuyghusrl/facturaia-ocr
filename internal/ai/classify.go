package ai

import (
	"fmt"
	"strings"

	"github.com/facturaIA/invoice-ocr-service/internal/models"
)

// ClassifyTipoFactura overrides invoice.TipoFactura based on whether the
// emisor_rnc matches the empresa_rnc (the scanning company's own RNC).
//
// Lógica DGII:
//   - emisor_rnc == empresa_rnc  → factura EMITIDA por la empresa → "ventas" (aplica 607)
//   - emisor_rnc != empresa_rnc  → factura RECIBIDA por la empresa → "gastos" (aplica 606)
//   - emisor_rnc vacío            → no hay suficiente info; conserva el valor actual sin override
//
// Nota: empresaRNC debe venir limpio (sin guiones), igual que invoice.RNCEmisor.
func ClassifyTipoFactura(invoice *models.Invoice, empresaRNC string) {
	if invoice == nil || invoice.RNCEmisor == "" || empresaRNC == "" {
		// No hay info suficiente — no sobreescribimos
		return
	}

	normalizedEmisor := strings.ReplaceAll(invoice.RNCEmisor, "-", "")
	normalizedEmpresa := strings.ReplaceAll(empresaRNC, "-", "")

	if normalizedEmisor == normalizedEmpresa {
		invoice.TipoFactura = "ventas"
		fmt.Printf("[OCR-Classify] override tipoFactura='ventas': emisor_rnc=%s == empresa_rnc=%s\n",
			invoice.RNCEmisor, empresaRNC)
	} else {
		invoice.TipoFactura = "gastos"
	}
}
