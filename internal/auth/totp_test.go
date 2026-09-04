package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func codeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, at, totpValidateOpts)
	if err != nil {
		t.Fatalf("GenerateCodeCustom() error = %v", err)
	}
	return code
}

func TestVerifyTOTPCode_ExactAndSkewWindow(t *testing.T) {
	secret, err := newTOTPSecret("admin")
	if err != nil {
		t.Fatalf("newTOTPSecret() error = %v", err)
	}
	now := time.Now()
	wantStep := now.Unix() / totpPeriod

	if step, ok := verifyTOTPCode(secret, codeAt(t, secret, now), now); !ok || step != wantStep {
		t.Fatalf("current-period code: ok=%v step=%d, want ok=true step=%d", ok, step, wantStep)
	}
	if step, ok := verifyTOTPCode(secret, codeAt(t, secret, now.Add(-30*time.Second)), now); !ok || step != wantStep-1 {
		t.Fatalf("-1 period code: ok=%v step=%d, want ok=true step=%d", ok, step, wantStep-1)
	}
	if step, ok := verifyTOTPCode(secret, codeAt(t, secret, now.Add(30*time.Second)), now); !ok || step != wantStep+1 {
		t.Fatalf("+1 period code: ok=%v step=%d, want ok=true step=%d", ok, step, wantStep+1)
	}
	if _, ok := verifyTOTPCode(secret, codeAt(t, secret, now.Add(-90*time.Second)), now); ok {
		t.Fatal("-3 period code (outside ±1 window) verified, want rejection")
	}
	if _, ok := verifyTOTPCode(secret, "000000", now); ok {
		// Astronomically unlikely to collide with the real code, but guard
		// against it rather than assert flakily.
		if codeAt(t, secret, now) == "000000" {
			t.Skip("random secret happened to produce 000000 as the real code")
		}
		t.Fatal("wrong code verified, want rejection")
	}
	if _, ok := verifyTOTPCode(secret, "", now); ok {
		t.Fatal("empty code verified, want rejection")
	}
}

func TestOTPAuthURL_ContainsExpectedParams(t *testing.T) {
	uri := otpauthURL("admin", "JBSWY3DPEHPK3PXP")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("otpauthURL() = %q, want otpauth://totp/ prefix", uri)
	}
	for _, want := range []string{"secret=JBSWY3DPEHPK3PXP", "issuer=UCollection", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(uri, want) {
			t.Fatalf("otpauthURL() = %q, missing %q", uri, want)
		}
	}
}

func TestTOTPQRCodePNG_ProducesValidPNG(t *testing.T) {
	png, err := totpQRCodePNG("admin", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("totpQRCodePNG() error = %v", err)
	}
	pngMagic := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if len(png) < len(pngMagic) || string(png[:len(pngMagic)]) != string(pngMagic) {
		t.Fatal("totpQRCodePNG() did not produce a PNG-magic-prefixed payload")
	}
}
