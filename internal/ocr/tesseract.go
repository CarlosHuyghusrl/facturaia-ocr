package ocr

import (
)

// TesseractOCR implements a mock OCR engine for Windows compatibility
// This avoids the need for CGO/GCC/Tesseract installation.
type TesseractOCR struct {
	language string
}

// NewTesseractOCR creates a new Tesseract OCR instance (Mock)
func NewTesseractOCR(language string) *TesseractOCR {
	if language == "" {
		language = "eng" // Default to English
	}
	return &TesseractOCR{
		language: language,
	}
}

// ExtractText performs OCR on preprocessed image bytes (Mock implementation)
func (t *TesseractOCR) ExtractText(imageBytes []byte) (string, float64, error) {
	// Since we are running on Windows without Tesseract installed, we return a placeholder.
	// The system should be configured to use Gemini/OpenAI primarily.
	return "OCR unavailable in local mode. Please use AI provider.", 0.0, nil
}

// ExtractTextWithDetails returns text and detailed word information (Mock implementation)
func (t *TesseractOCR) ExtractTextWithDetails(imageBytes []byte) (string, []WordInfo, error) {
	return "OCR unavailable in local mode.", nil, nil
}

// WordInfo contains detailed information about a detected word
type WordInfo struct {
	Text       string
	Confidence float64
	Box        BoundingBox
}

// BoundingBox represents the location of text in the image
type BoundingBox struct {
	X      int
	Y      int
	Width  int
	Height int
}
