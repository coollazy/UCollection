package auth

import (
	"crypto/subtle"
	"net/url"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
)

// totpIssuer is embedded in the otpauth:// URI shown to Google
// Authenticator-style clients.
const totpIssuer = "UCollection"

// totpPeriod/totpSkew implement 技術架構設計第9節「TOTP驗證參數」: standard
// 30-second steps, ±1 period acceptance window (90 seconds total).
const totpPeriod = 30
const totpSkew = 1

var totpValidateOpts = totp.ValidateOpts{
	Period:    totpPeriod,
	Skew:      totpSkew,
	Digits:    otp.DigitsSix,
	Algorithm: otp.AlgorithmSHA1,
}

// newTOTPSecret generates a fresh base32 secret via pquerna/otp (技術架構
// 設計第9節: "用pquerna/otp...產生新secret"). The returned *otp.Key is only
// used for its Secret() — QR display always goes through otpauthURL below
// so that the setup page can be safely reloaded (see totpSetupPageHandler)
// without needing to persist the whole otp.Key, just the raw secret string
// already being stored in admin_sessions.pending_totp_secret.
func newTOTPSecret(accountName string) (string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: accountName,
	})
	if err != nil {
		return "", err
	}
	return key.Secret(), nil
}

// otpauthURL rebuilds the otpauth:// URI from a persisted secret — used
// for every QR render (first display and any page reload alike), so
// there's exactly one code path for it.
func otpauthURL(accountName, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", totpIssuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	u := url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + totpIssuer + ":" + accountName,
		RawQuery: v.Encode(),
	}
	return u.String()
}

func totpQRCodePNG(accountName, secret string) ([]byte, error) {
	return qrcode.Encode(otpauthURL(accountName, secret), qrcode.Medium, 256)
}

// verifyTOTPCode checks code against secret within the ±1 period window
// and, on success, returns the RFC6238 time-step counter that matched —
// needed by callers to enforce replay protection via
// admin_account.last_totp_step (技術架構設計第9節「重放防護」：
// pquerna/otp's ValidateCustom only returns a bool, not which step in the
// skew window matched, so this reimplements the window check itself).
func verifyTOTPCode(secret, code string, at time.Time) (step int64, ok bool) {
	if code == "" {
		return 0, false
	}
	center := at.Unix() / totpPeriod
	for _, s := range []int64{center - totpSkew, center, center + totpSkew} {
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0), totpValidateOpts)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}
