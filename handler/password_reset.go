
package handler

import (
	"net/http"
	"strings"

	"github.com/Supattarblue/bookmellow-backend/auth"
)

func (h *Handler) ForgotPasswordDirect(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email       string `json:"email"`
		NewPassword string `json:"new_password"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	email := strings.TrimSpace(input.Email)

	if email == "" {
		writeError(w, http.StatusBadRequest, "กรุณากรอกอีเมล")
		return
	}

	if len(input.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "รหัสผ่านต้องมีอย่างน้อย 8 ตัวอักษร")
		return
	}

	user, err := h.Store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "ไม่พบอีเมลสมาชิกนี้")
			return
		}

		internal(w, err)
		return
	}

	passwordHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		internal(w, err)
		return
	}

	if err := h.Store.UpdatePasswordByUserID(
		r.Context(),
		user.ID,
		passwordHash,
	); err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "เปลี่ยนรหัสผ่านสำเร็จ กรุณาเข้าสู่ระบบใหม่",
	})
}
