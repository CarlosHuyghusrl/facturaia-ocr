package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/facturaIA/invoice-ocr-service/internal/db"
)

// LoginRequest represents the login request body
type LoginRequest struct {
	EmpresaAlias string `json:"empresa_alias"`
	Email        string `json:"email"`
	Password     string `json:"password"`
}

// LoginResponse represents the successful login response
type LoginResponse struct {
	Token         string `json:"token"`
	UserID        string `json:"user_id"`
	Email         string `json:"email"`
	Nombre        string `json:"nombre"`
	Rol           string `json:"rol"`
	EmpresaAlias  string `json:"empresa_alias"`
	EmpresaNombre string `json:"empresa_nombre"`
}

// LoginHandler handles user authentication
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.EmpresaAlias == "" || req.Email == "" || req.Password == "" {
		http.Error(w, `{"error":"empresa_alias, email and password are required"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Call PostgreSQL function verificar_login
	query := `SELECT user_id, email, nombre, rol, empresa_alias, empresa_nombre 
             FROM public.verificar_login($1, $2, $3)`

	var userID, email, nombre, rol, empresaAlias, empresaNombre string
	err := db.Pool.QueryRow(ctx, query, req.EmpresaAlias, req.Email, req.Password).Scan(
		&userID, &email, &nombre, &rol, &empresaAlias, &empresaNombre,
	)

	if err != nil {
		// No user found or wrong password
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Generate JWT
	token, err := GenerateToken(userID, email, empresaAlias, empresaNombre, rol)
	if err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	// Update last login in background
	go func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		db.Pool.Exec(ctx2, "SELECT public.registrar_login($1, $2::uuid)", empresaAlias, userID)
	}()

	json.NewEncoder(w).Encode(LoginResponse{
		Token:         token,
		UserID:        userID,
		Email:         email,
		Nombre:        nombre,
		Rol:           rol,
		EmpresaAlias:  empresaAlias,
		EmpresaNombre: empresaNombre,
	})
}
