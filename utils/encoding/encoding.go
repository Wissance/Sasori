package encoding

import (
	"crypto/sha512"
	"encoding/base64"
	"hash"
	"math/rand"
)

type PasswordJsonEncoder struct {
	salt   string
	hasher hash.Hash
}

func NewPasswordJsonEncoder(salt string) PasswordJsonEncoder {
	encoder := PasswordJsonEncoder{
		hasher: sha512.New(),
		salt:   salt,
	}
	return encoder
}

func (e *PasswordJsonEncoder) GetB64PasswordHash(password string) string {
	if IsPasswordHashed(password) {
		return password
	}
	passwordBytes := []byte(password + e.salt)
	e.hasher.Write(passwordBytes)
	hashedPasswordBytes := e.hasher.Sum(nil)
	e.hasher.Reset()

	b64encoded := b64Encode(hashedPasswordBytes)
	return b64encoded
}

func (e *PasswordJsonEncoder) IsPasswordsMatch(password, hash string) bool {
	currPasswordHash := e.GetB64PasswordHash(password)
	return b64Decode(hash) == b64Decode(currPasswordHash)
}

func GenerateRandomSalt() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+="
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = charset[rand.Intn(len(charset))]
	}
	return string(salt)
}

func IsPasswordHashed(password string) bool {
	decoded := b64Decode(password)
	if len(decoded) == 0 {
		return false
	}
	return true
}

func b64Encode(encoded []byte) string {
	cstr := base64.URLEncoding.EncodeToString(encoded)
	return cstr
}

func b64Decode(encoded string) string {
	cstr, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(cstr)
}
