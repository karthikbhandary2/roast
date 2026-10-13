package handlers

import (
	"context"
	"net/http"
	"time"

	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/middleware"
)

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// Register handles POST /v1/auth/register.
func (d *Deps) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	resp, err := middleware.Do(r.Context(), d.UserBreaker, func(ctx context.Context) (*userv1.RegisterResponse, error) {
		return d.User.Register(ctx, &userv1.RegisterRequest{
			Email:       req.Email,
			Password:    req.Password,
			DisplayName: req.DisplayName,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusCreated, resp.User)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken     string `json:"access_token"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

// Login handles POST /v1/auth/login. The refresh token goes in an httpOnly
// cookie, not the JSON body - LLD.md §3.1.
func (d *Deps) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	resp, err := middleware.Do(r.Context(), d.UserBreaker, func(ctx context.Context) (*userv1.AuthenticateResponse, error) {
		return d.User.Authenticate(ctx, &userv1.AuthenticateRequest{
			Email:    req.Email,
			Password: req.Password,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    resp.RefreshToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/v1/auth",
		MaxAge:   30 * 24 * 60 * 60, // 30 days, matches refresh_tokens.expires_at in User Service
	})

	writeData(w, http.StatusOK, loginResponse{
		AccessToken:      resp.AccessToken,
		ExpiresInSeconds: resp.ExpiresInSeconds,
	})
}

// Logout handles POST /v1/auth/logout. Resolves LLD.md §9 item 4 - sets
// tokens_valid_after server-side and clears the refresh cookie client-side.
func (d *Deps) Logout(w http.ResponseWriter, r *http.Request) {
	claims := mustClaims(r)
	cookie, err := r.Cookie("refresh_token")
	refreshToken := ""
	if err == nil {
		refreshToken = cookie.Value
	}

	_, callErr := middleware.Do(authContext(r), d.UserBreaker, func(ctx context.Context) (*userv1.LogoutResponse, error) {
		return d.User.Logout(ctx, &userv1.LogoutRequest{
			UserId:       claims.UserID,
			RefreshToken: refreshToken,
		})
	})
	if callErr != nil {
		writeError(w, r, callErr)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/v1/auth",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}
