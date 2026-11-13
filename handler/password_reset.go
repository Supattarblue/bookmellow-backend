package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
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

	if err := sendPasswordResetEmail(email, resetLink); err != nil {
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

func sendPasswordResetEmail(to, resetLink string) error {
	gmailUser := os.Getenv("GMAIL_USER")
	gmailAppPassword := os.Getenv("GMAIL_APP_PASSWORD")

	if gmailUser == "" || gmailAppPassword == "" {
		return fmt.Errorf("GMAIL_USER or GMAIL_APP_PASSWORD is not configured")
	}

	auth := smtp.PlainAuth(
		"",
		gmailUser,
		gmailAppPassword,
		"smtp.gmail.com",
	)

	subject := "BookMellow - Password Reset"
	body := fmt.Sprintf(
		"Hello,\n\n"+
			"We received a request to reset your BookMellow password.\n\n"+
			"Open the link below to create a new password:\n%s\n\n"+
			"This link expires in 15 minutes and can only be used once.\n\n"+
			"If you did not request this, you can ignore this email.\n\n"+
			"BookMellow Team",
		resetLink,
	)

	message := []byte(
		"From: BookMellow <" + gmailUser + ">\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" +
			body,
	)

	return smtp.SendMail(
		"smtp.gmail.com:587",
		auth,
		gmailUser,
		[]string{to},
		message,
	)
}
