package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/users"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		respond.Error(w, http.StatusBadRequest, "INVALID_BODY", "cuerpo inválido")
		return false
	}
	return true
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		respond.Error(w, http.StatusBadRequest, "INVALID_REGISTER", "email, password y full_name son obligatorios")
		return
	}
	u, tokens, err := h.service.Register(r.Context(), req.Email, req.Password, req.FullName)
	if err != nil {
		if errors.Is(err, users.ErrEmailTaken) {
			respond.Error(w, http.StatusConflict, "EMAIL_TAKEN", "el email ya está registrado")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "REGISTER_FAILED", "error al registrar usuario")
		return
	}
	respond.JSON(w, http.StatusCreated, map[string]any{
		"user": map[string]string{
			"id":        u.ID,
			"email":     u.Email,
			"full_name": u.FullName,
		},
		"tokens": tokens,
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	u, tokens, err := h.service.Login(r.Context(), strings.TrimSpace(strings.ToLower(req.Email)), req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			respond.Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "credenciales inválidas")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "LOGIN_FAILED", "error al iniciar sesión")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]string{
			"id":        u.ID,
			"email":     u.Email,
			"full_name": u.FullName,
		},
		"tokens": tokens,
	})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decode(w, r, &req) {
		return
	}
	tokens, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidRefresh) {
			respond.Error(w, http.StatusUnauthorized, "INVALID_REFRESH", "token de refresco inválido")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "REFRESH_FAILED", "error al renovar sesión")
		return
	}
	respond.JSON(w, http.StatusOK, tokens)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decode(w, r, &req) {
		return
	}
	_ = h.service.Logout(r.Context(), req.RefreshToken)
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
