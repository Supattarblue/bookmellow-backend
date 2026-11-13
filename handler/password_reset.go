package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Supattarblue/bookmellow-backend/auth"
)

const passwordResetMessage = "If the email is registered, a password reset link will be sent."

func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	email := strings.TrimSpace(input.Email)
	if email == "" {
		writeError(w, http.StatusBadRequest, "Email is required")
		return
	}

	user, err := h.Store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]string{
				"message": passwordResetMessage,
			})
			return
		}

		internal(w, err)
		return
	}

	// Generate a secure random token.
	randomToken := make([]byte, 32)
	if _, err := rand.Read(randomToken); err != nil {
		internal(w, err)
		return
	}

	rawToken := hex.EncodeToString(randomToken)

	// Store only the SHA-256 hash of the token.
	tokenHashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])

	expiresAt := time.Now().Add(15 * time.Minute)

	if err := h.Store.CreatePasswordResetToken(
		r.Context(),
		user.ID,
		tokenHash,
		expiresAt,
	); err != nil {
		internal(w, err)
		return
	}

	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	if frontendURL == "" {
		log.Println("Password reset failed: FRONTEND_URL is not configured")
		writeJSON(w, http.StatusOK, map[string]string{
			"message": passwordResetMessage,
		})
		return
	}

	resetLink := frontendURL + "/reset-password?token=" + url.QueryEscape(rawToken)

	if err := sendPasswordResetEmail(r.Context(), email, resetLink); err != nil {
		log.Printf("Password reset email could not be sent: %v", err)

		writeJSON(w, http.StatusOK, map[string]string{
			"message": passwordResetMessage,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": passwordResetMessage,
	})
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	input.Token = strings.TrimSpace(input.Token)

	if input.Token == "" || len(input.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "Token and password (at least 8 characters) are required")
		return
	}

	// Hash the received token before looking it up.
	tokenHashBytes := sha256.Sum256([]byte(input.Token))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])

	passwordHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		internal(w, err)
		return
	}

	err = h.Store.ResetPassword(
		r.Context(),
		tokenHash,
		passwordHash,
	)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusBadRequest, "Invalid or expired reset link")
			return
		}

		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password has been reset successfully",
	})
}

// sendPasswordResetEmail sends an email using the Resend HTTPS API.
func sendPasswordResetEmail(ctx context.Context, to, resetLink string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	fromEmail := os.Getenv("RESEND_FROM_EMAIL")

	if apiKey == "" {
		return fmt.Errorf("RESEND_API_KEY is not configured")
	}

	if fromEmail == "" {
		return fmt.Errorf("RESEND_FROM_EMAIL is not configured")
	}

	payload := map[string]interface{}{
		"from": fromEmail,
		"to": []string{
			to,
		},
		"subject": "BookMellow - Password Reset",
		"text": fmt.Sprintf(
			"Hello,\n\n"+
				"We received a request to reset your BookMellow password.\n\n"+
				"Open the link below to create a new password:\n%s\n\n"+
				"This link expires in 15 minutes and can only be used once.\n\n"+
				"If you did not request this, you can ignore this email.\n\n"+
				"BookMellow Team",
			resetLink,
		),
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("could not encode email request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.resend.com/emails",
		bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		return fmt.Errorf("could not create Resend request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Resend API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"Resend API returned status %d: %s",
			resp.StatusCode,
			string(responseBody),
		)
	}

	return nil
}
