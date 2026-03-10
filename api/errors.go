package api

import (
	"encoding/json"
	"net/http"
)

// AppError represents a structured error response with user-friendly messages
type AppError struct {
	HTTPStatus  int    `json:"-"`
	ErrorCode   string `json:"error_code"`
	Message     string `json:"error"`
	UserMessage string `json:"user_message"`
}

// Error codes for the API
var (
	ErrAIUnavailable = AppError{
		HTTPStatus:  503,
		ErrorCode:   "ai_unavailable",
		Message:     "AI service is not available",
		UserMessage: "El servicio de IA no está disponible en este momento. Tu factura se guardó y se procesará automáticamente cuando el servicio se restablezca.",
	}
	ErrAIParseError = AppError{
		HTTPStatus:  200, // Still returns 200 because invoice is saved
		ErrorCode:   "ai_parse_error",
		Message:     "AI could not extract invoice data",
		UserMessage: "No pudimos extraer la información de esta imagen. Intenta con mejor iluminación o una foto más nítida.",
	}
	ErrStorageUnavailable = AppError{
		HTTPStatus:  503,
		ErrorCode:   "storage_unavailable",
		Message:     "Storage service is not available",
		UserMessage: "El almacenamiento no está disponible. Por favor intenta de nuevo en unos minutos.",
	}
	ErrDBUnavailable = AppError{
		HTTPStatus:  503,
		ErrorCode:   "db_unavailable",
		Message:     "Database is not available",
		UserMessage: "Hubo un problema guardando los datos. Tu imagen está segura y reintentaremos en unos minutos.",
	}
	ErrDBSaveError = AppError{
		HTTPStatus:  500,
		ErrorCode:   "db_save_error",
		Message:     "Failed to save invoice data",
		UserMessage: "Hubo un problema guardando los datos. Tu imagen está segura y reintentaremos en unos minutos.",
	}
	ErrFormatUnsupported = AppError{
		HTTPStatus:  400,
		ErrorCode:   "format_unsupported",
		Message:     "Unsupported file format",
		UserMessage: "Este formato de imagen no es compatible. Usa JPG, PNG o PDF.",
	}
	ErrTimeout = AppError{
		HTTPStatus:  504,
		ErrorCode:   "timeout",
		Message:     "Processing timed out",
		UserMessage: "El procesamiento está tardando más de lo normal. Tu factura está en cola y se procesará pronto.",
	}
	ErrAuthExpired = AppError{
		HTTPStatus:  401,
		ErrorCode:   "auth_expired",
		Message:     "Authentication token expired",
		UserMessage: "Tu sesión ha expirado. Por favor inicia sesión nuevamente.",
	}
	ErrFileTooLarge = AppError{
		HTTPStatus:  413,
		ErrorCode:   "file_too_large",
		Message:     "File exceeds maximum size",
		UserMessage: "La imagen es demasiado grande. El tamaño máximo es 20MB.",
	}
)

// sendJSON sends a JSON response with the given status code and data
func sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// sendAppError sends a structured error response
func sendAppError(w http.ResponseWriter, appErr AppError) {
	sendJSON(w, appErr.HTTPStatus, map[string]string{
		"error_code":   appErr.ErrorCode,
		"error":        appErr.Message,
		"user_message": appErr.UserMessage,
	})
}

// sendAppErrorWithDetail sends a structured error with additional detail
func sendAppErrorWithDetail(w http.ResponseWriter, appErr AppError, detail string) {
	sendJSON(w, appErr.HTTPStatus, map[string]string{
		"error_code":   appErr.ErrorCode,
		"error":        detail,
		"user_message": appErr.UserMessage,
	})
}
