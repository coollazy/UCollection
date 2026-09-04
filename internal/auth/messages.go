package auth

// reasonMessage maps a short "?reason=" redirect query value to the
// Chinese message shown on the page it redirects to. Using short codes in
// the URL rather than raw text keeps arbitrary text out of query strings
// (html/template still auto-escapes either way, but this is tidier and
// keeps all wording in one place).
func reasonMessage(reason string) string {
	switch reason {
	case "locked":
		return "驗證失敗次數過多，此次登入已失效，請重新開始"
	case "totp_setup_raced":
		return "2FA已由其他工作階段完成設定，請直接輸入驗證碼登入"
	default:
		return ""
	}
}
