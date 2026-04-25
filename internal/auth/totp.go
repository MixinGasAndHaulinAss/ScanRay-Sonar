package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// EnrollTOTP generates a fresh TOTP secret for the user and returns it
// alongside an otpauth:// URL (for manual entry) and a base64-encoded
// PNG QR code (for one-tap enrollment in Authenticator apps).
//
// The returned secret must be stored encrypted (Sealer.Seal) and only
// activated after the user proves possession by submitting a valid
// 6-digit code via VerifyTOTP.
func EnrollTOTP(issuer, accountName string) (secret, otpauthURL, qrPNGB64 string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", "", err
	}
	img, err := key.Image(256, 256)
	if err != nil {
		return "", "", "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", "", "", err
	}
	return key.Secret(), key.URL(), base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// VerifyTOTP returns nil iff `code` is a currently-valid TOTP for
// `secret`. A small skew is tolerated by the underlying library.
func VerifyTOTP(secret, code string) error {
	if !totp.Validate(code, secret) {
		return errors.New("auth: invalid TOTP code")
	}
	return nil
}
