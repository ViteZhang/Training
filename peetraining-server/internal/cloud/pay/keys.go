package pay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// pemBlock 允许环境变量里只放 Base64 主体（支付宝控制台给的就是这种），也允许完整 PEM；\n 字面量还原为换行。
func pemBlock(s, kind string) ([]byte, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, `\n`, "\n"))
	if s == "" {
		return nil, errors.New("密钥为空")
	}
	if !strings.Contains(s, "-----BEGIN") {
		s = "-----BEGIN " + kind + "-----\n" + s + "\n-----END " + kind + "-----"
	}
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		return nil, errors.New("不是合法的 PEM")
	}
	return b.Bytes, nil
}

// ParseRSAPrivateKey 解析 PKCS#8 或 PKCS#1 私钥。
func ParseRSAPrivateKey(s string) (*rsa.PrivateKey, error) {
	der, err := pemBlock(s, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("不是 RSA 私钥")
	}
	return x509.ParsePKCS1PrivateKey(der)
}

// ParseRSAPublicKey 解析 PKIX 公钥或 X.509 证书里的公钥（微信平台证书、支付宝公钥）。
func ParseRSAPublicKey(s string) (*rsa.PublicKey, error) {
	kind := "PUBLIC KEY"
	if strings.Contains(s, "CERTIFICATE") {
		kind = "CERTIFICATE"
	}
	der, err := pemBlock(s, kind)
	if err != nil {
		return nil, err
	}
	if kind == "CERTIFICATE" {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		if k, ok := c.PublicKey.(*rsa.PublicKey); ok {
			return k, nil
		}
		return nil, errors.New("证书里不是 RSA 公钥")
	}
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	if rk, ok := k.(*rsa.PublicKey); ok {
		return rk, nil
	}
	return nil, errors.New("不是 RSA 公钥")
}

// signSHA256 是 RSA PKCS#1 v1.5 + SHA-256 签名（微信 APIv3、支付宝 RSA2 都用它），结果 Base64。
func signSHA256(k *rsa.PrivateKey, msg string) (string, error) {
	h := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func verifySHA256(k *rsa.PublicKey, msg, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return fmt.Errorf("%w：签名不是 Base64", ErrInvalidSignature)
	}
	h := sha256.Sum256([]byte(msg))
	if err := rsa.VerifyPKCS1v15(k, crypto.SHA256, h[:], sig); err != nil {
		return ErrInvalidSignature
	}
	return nil
}
