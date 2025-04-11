package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncryption(t *testing.T) {

	t.Run("successful decryption", func(t *testing.T) {
		t.Skip("Needs a AWS login to work")
		service := NewDefaultEncryptionService(nil)
		encrypted, err := service.Encrypt("test", "arn:aws:kms:eu-central-1:266735805679:key/81734ed9-e4f9-40ac-b3bc-9c16a5c2963e")
		assert.NoError(t, err)

		decrypted, err := service.Decrypt(encrypted, "arn:aws:kms:eu-central-1:266735805679:key/81734ed9-e4f9-40ac-b3bc-9c16a5c2963e")
		assert.NoError(t, err)
		assert.Equal(t, "test", decrypted)
	})
}
