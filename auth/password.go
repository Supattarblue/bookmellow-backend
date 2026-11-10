
package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword แปลงรหัสผ่านเป็น bcrypt hash
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword ตรวจสอบว่ารหัสผ่านตรงกับ hash ที่เก็บไว้หรือไม่
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
