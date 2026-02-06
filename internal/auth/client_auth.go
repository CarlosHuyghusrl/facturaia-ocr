package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"log"

	"golang.org/x/crypto/bcrypt"
	"github.com/facturaIA/invoice-ocr-service/internal/db"
)

// ClientLoginRequest - Login con RNC y PIN para clientes
type ClientLoginRequest struct {
	RNC string `json:"rnc"`
	PIN string `json:"pin"`
}

// ClientLoginResponse - Respuesta de login para clientes
type ClientLoginResponse struct {
	Success bool        `json:"success"`
	Token   string      `json:"token,omitempty"`
	Cliente *ClientInfo `json:"cliente,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ClientInfo - Datos del cliente
type ClientInfo struct {
	ID           string `json:"id"`
	Nombre       string `json:"nombre"`
	RNC          string `json:"rnc"`
	Email        string `json:"email,omitempty"`
	Telefono     string `json:"telefono,omitempty"`
	Direccion    string `json:"direccion,omitempty"`
	UltimoAcceso string `json:"ultimoAcceso,omitempty"`
}

// ClientMeResponse - Respuesta de verificar sesion
type ClientMeResponse struct {
	Success bool         `json:"success"`
	Cliente *ClientInfo  `json:"cliente,omitempty"`
	Stats   *ClientStats `json:"stats,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// ClientStats - Estadisticas del cliente
type ClientStats struct {
	FacturasPendientes int `json:"facturasPendientes"`
	FacturasProcesadas int `json:"facturasProcesadas"`
}

// ClientLoginHandler - POST /api/clientes/login/
func ClientLoginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Verificar que la base de datos esté disponible
	if db.Pool == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ClientLoginResponse{
			Success: false,
			Error:   "Servicio de autenticación no disponible",
		})
		return
	}

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(ClientLoginResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req ClientLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(ClientLoginResponse{
			Success: false,
			Error:   "invalid request body",
		})
		return
	}

	// Limpiar RNC (quitar guiones)
	rnc := strings.ReplaceAll(req.RNC, "-", "")

	if rnc == "" || req.PIN == "" {
		json.NewEncoder(w).Encode(ClientLoginResponse{
			Success: false,
			Error:   "RNC y PIN son requeridos",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Buscar cliente por RNC (normalizado sin guiones)
	query := `SELECT id, razon_social, rnc_cedula, email, telefono, direccion, 
	                 pin_hash, ultimo_acceso_app, owner_id
	          FROM clientes
	          WHERE REPLACE(rnc_cedula, '-', '') = $1 
	          AND app_activa = true 
	          AND (pin_bloqueado_hasta IS NULL OR pin_bloqueado_hasta < NOW())`

	var id, nombre, rncDB string
	var email, telefono, direccion, pinHash *string
	var ultimoAcceso *time.Time
	var ownerID *string

	err := db.Pool.QueryRow(ctx, query, rnc).Scan(
		&id, &nombre, &rncDB, &email, &telefono, &direccion, &pinHash, &ultimoAcceso, &ownerID,
	)

	log.Printf("Query result - err: %v, pinHash nil: %v", err, pinHash == nil)
	if err != nil || pinHash == nil {
		json.NewEncoder(w).Encode(ClientLoginResponse{
			Success: false,
			Error:   "RNC o PIN incorrectos",
		})
		return
	}

	// Verificar PIN con bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(*pinHash), []byte(req.PIN)); err != nil {
		// Incrementar intentos fallidos
		go func() {
			ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel2()
			db.Pool.Exec(ctx2, `UPDATE clientes SET pin_intentos = COALESCE(pin_intentos, 0) + 1,
			                   pin_bloqueado_hasta = CASE WHEN COALESCE(pin_intentos, 0) >= 4 
			                   THEN NOW() + INTERVAL '30 minutes' ELSE NULL END
			                   WHERE id = $1::uuid`, id)
		}()
		json.NewEncoder(w).Encode(ClientLoginResponse{
			Success: false,
			Error:   "RNC o PIN incorrectos",
		})
		return
	}

	// Generar JWT para el cliente
	token, err := GenerateToken(id, safeString(email), safeString(ownerID), nombre, "cliente")
	if err != nil {
		json.NewEncoder(w).Encode(ClientLoginResponse{
			Success: false,
			Error:   "error generando token",
		})
		return
	}

	// Limpiar intentos fallidos y actualizar ultimo acceso
	go func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		db.Pool.Exec(ctx2, `UPDATE clientes SET 
		    ultimo_acceso_app = NOW(), 
		    pin_intentos = 0, 
		    pin_bloqueado_hasta = NULL 
		    WHERE id = $1::uuid`, id)
	}()

	var ultimoAccesoStr string
	if ultimoAcceso != nil {
		ultimoAccesoStr = ultimoAcceso.Format(time.RFC3339)
	}

	json.NewEncoder(w).Encode(ClientLoginResponse{
		Success: true,
		Token:   token,
		Cliente: &ClientInfo{
			ID:           id,
			Nombre:       nombre,
			RNC:          rncDB,
			Email:        safeString(email),
			Telefono:     safeString(telefono),
			Direccion:    safeString(direccion),
			UltimoAcceso: ultimoAccesoStr,
		},
	})
}

// ClientMeHandler - GET /api/clientes/me/
func ClientMeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Verificar que la base de datos esté disponible
	if db.Pool == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ClientMeResponse{
			Success: false,
			Error:   "Servicio no disponible",
		})
		return
	}

	ctx := r.Context()
	claims, err := GetClaimsFromContext(ctx)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ClientMeResponse{
			Success: false,
			Error:   "unauthorized",
		})
		return
	}

	// Obtener datos actualizados del cliente
	query := `SELECT id, razon_social, rnc_cedula, email, telefono, direccion, ultimo_acceso_app
	          FROM clientes
	          WHERE id = $1::uuid AND app_activa = true`

	var id, nombre, rnc string
	var email, telefono, direccion *string
	var ultimoAcceso *time.Time

	err = db.Pool.QueryRow(ctx, query, claims.UserID).Scan(
		&id, &nombre, &rnc, &email, &telefono, &direccion, &ultimoAcceso,
	)

	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ClientMeResponse{
			Success: false,
			Error:   "cliente no encontrado",
		})
		return
	}

	// Obtener estadisticas de facturas_clientes
	statsQuery := `SELECT 
	    COUNT(*) FILTER (WHERE estado = 'pendiente') as pendientes,
	    COUNT(*) FILTER (WHERE estado IN ('procesado', 'validado')) as procesadas
	FROM facturas_clientes WHERE cliente_id = $1::uuid`

	var pendientes, procesadas int
	db.Pool.QueryRow(ctx, statsQuery, claims.UserID).Scan(&pendientes, &procesadas)

	var ultimoAccesoStr string
	if ultimoAcceso != nil {
		ultimoAccesoStr = ultimoAcceso.Format(time.RFC3339)
	}

	json.NewEncoder(w).Encode(ClientMeResponse{
		Success: true,
		Cliente: &ClientInfo{
			ID:           id,
			Nombre:       nombre,
			RNC:          rnc,
			Email:        safeString(email),
			Telefono:     safeString(telefono),
			Direccion:    safeString(direccion),
			UltimoAcceso: ultimoAccesoStr,
		},
		Stats: &ClientStats{
			FacturasPendientes: pendientes,
			FacturasProcesadas: procesadas,
		},
	})
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
