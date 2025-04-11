package utils

import (
	"context"
	"encoding/base64"

	mpl "github.com/aws/aws-cryptographic-material-providers-library/releases/go/mpl/awscryptographymaterialproviderssmithygenerated"
	mpltypes "github.com/aws/aws-cryptographic-material-providers-library/releases/go/mpl/awscryptographymaterialproviderssmithygeneratedtypes"
	client "github.com/aws/aws-encryption-sdk/releases/go/encryption-sdk/awscryptographyencryptionsdksmithygenerated"
	esdktypes "github.com/aws/aws-encryption-sdk/releases/go/encryption-sdk/awscryptographyencryptionsdksmithygeneratedtypes"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"go.uber.org/zap"
)

// See: https://github.com/aws/aws-encryption-sdk/blob/mainline/releases/go/encryption-sdk/examples/keyring/awskmskeyring/awskmskeyring.go

type EncryptionService interface {
	Encrypt(plainText string, kmsKeyARN string) (string, error)
	Decrypt(encryptedText string, kmsKeyARN string) (string, error)
}

type DefaultEncryptionService struct {
	logger *zap.Logger
}

func NewDefaultEncryptionService(logger *zap.Logger) *DefaultEncryptionService {
	return &DefaultEncryptionService{
		logger: logger,
	}
}

func (e DefaultEncryptionService) Decrypt(encryptedText string, kmsKeyARN string) (string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		e.logger.Error("failed to load default config", zap.Error(err))
		return "", err
	}
	matProv, err := mpl.NewClient(mpltypes.MaterialProvidersConfig{})
	if err != nil {
		e.logger.Error("failed to create material provider", zap.Error(err))
		return "", err
	}
	awsKmsKeyringInput := mpltypes.CreateAwsKmsKeyringInput{
		KmsClient: kms.NewFromConfig(cfg),
		KmsKeyId:  kmsKeyARN,
	}
	awsKmsKeyring, err := matProv.CreateAwsKmsKeyring(context.Background(), awsKmsKeyringInput)
	if err != nil {
		e.logger.Error("failed to create AWS KMS keyring", zap.Error(err))
		return "", err
	}

	encryptionClient, err := client.NewClient(esdktypes.AwsEncryptionSdkConfig{})
	if err != nil {
		e.logger.Error("failed to create encryption client", zap.Error(err))
		return "", err
	}

	byteString, _ := base64.StdEncoding.DecodeString(encryptedText)
	decryptOutput, err := encryptionClient.Decrypt(context.Background(), esdktypes.DecryptInput{
		Keyring:    awsKmsKeyring,
		Ciphertext: byteString,
		EncryptionContext: map[string]string{
			"Env": "Dev",
		},
	})
	if err != nil {
		e.logger.Error("failed to decrypt text", zap.Error(err))
		return "", err
	}

	return string(decryptOutput.Plaintext), nil
}

func (e DefaultEncryptionService) Encrypt(plainText string, kmsKeyARN string) (string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		e.logger.Error("failed to load default config", zap.Error(err))
		return "", err
	}
	matProv, err := mpl.NewClient(mpltypes.MaterialProvidersConfig{})
	if err != nil {
		e.logger.Error("failed to create material provider", zap.Error(err))
		return "", err
	}
	awsKmsKeyringInput := mpltypes.CreateAwsKmsKeyringInput{
		KmsClient: kms.NewFromConfig(cfg),
		KmsKeyId:  kmsKeyARN,
	}
	awsKmsKeyring, err := matProv.CreateAwsKmsKeyring(context.Background(), awsKmsKeyringInput)
	if err != nil {
		e.logger.Error("failed to create AWS KMS keyring", zap.Error(err))
		return "", err
	}
	encryptionClient, err := client.NewClient(esdktypes.AwsEncryptionSdkConfig{})
	if err != nil {
		e.logger.Error("failed to create encryption client", zap.Error(err))
		return "", err
	}
	res, err := encryptionClient.Encrypt(context.Background(), esdktypes.EncryptInput{
		Plaintext: []byte(plainText),
		EncryptionContext: map[string]string{
			"Env": "Dev",
		},
		Keyring: awsKmsKeyring,
	})
	if err != nil {
		e.logger.Error("failed to encrypt text", zap.Error(err))
		return "", err
	}
	plainText = base64.StdEncoding.EncodeToString(res.Ciphertext)

	return plainText, nil
}
