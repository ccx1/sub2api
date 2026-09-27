package openaiauth

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestTOTPKnownVector 用 RFC/常见 base32 密钥核验 TOTP 与 Python 版一致（固定时间点）。
func TestTOTPAt(t *testing.T) {
	// JBSWY3DPEHPK3PXP 是常见测试密钥；固定 unix=59 时步长=1。
	got, err := totpAt("JBSWY3DPEHPK3PXP", 59)
	if err != nil {
		t.Fatalf("totp err: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("totp len = %q", got)
	}
	// 空白/小写应等价
	got2, _ := totpAt("jbswy3dpehpk3pxp", 59)
	if got != got2 {
		t.Fatalf("case/space normalization mismatch: %q vs %q", got, got2)
	}
}

func TestFNV1a(t *testing.T) {
	// 与 Python format(h,'08x') 一致：这里只验证长度与确定性。
	a := fnv1a32("hello")
	b := fnv1a32("hello")
	if a != b || len(a) != 8 {
		t.Fatalf("fnv1a unstable/len: %q %q", a, b)
	}
}

func TestXORDecryptRoundTrip(t *testing.T) {
	key := "secretkey"
	plain := `[[2,3,4],[7,3]]`
	enc := xorDecrypt(plain, key) // XOR 自反
	dec := xorDecrypt(enc, key)
	if dec != plain {
		t.Fatalf("xor roundtrip failed: %q", dec)
	}
}

// TestTurnstileVMResolve 构造一个最小 dx 程序：opcode 2 SET reg1=payload，opcode 7 调用 reg3(resolve, reg1)。
func TestTurnstileVMResolve(t *testing.T) {
	reqToken := "tok123"
	// 程序：[[2,1,"OK"],[7,3,1]] —— 但 7 会 deref 参数，reg1="OK"。
	program := []any{
		[]any{2, 1, "OK"},
		[]any{7, 3, 1},
	}
	raw, _ := json.Marshal(program)
	enc := xorDecrypt(string(raw), reqToken)
	dxB64 := base64.StdEncoding.EncodeToString([]byte(enc))

	got, err := runTurnstileVM(dxB64, reqToken, "UA", "dev")
	if err != nil {
		t.Fatalf("vm err: %v", err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(got)
	if string(decoded) != "OK" {
		t.Fatalf("vm result = %q (want OK)", string(decoded))
	}
}

func TestPKCEAndAuthURL(t *testing.T) {
	u := authorizeURL("ws-123", "state-abc", "verifier-xyz")
	if u == "" || len(u) < 40 {
		t.Fatalf("auth url too short: %q", u)
	}
}

func TestJWTClaims(t *testing.T) {
	payload := map[string]any{"exp": float64(1735689600), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "ws-9"}}
	pb, _ := json.Marshal(payload)
	token := "h." + base64.RawURLEncoding.EncodeToString(pb) + ".s"
	claims, err := jwtClaims(token)
	if err != nil {
		t.Fatalf("claims err: %v", err)
	}
	if claims["exp"].(float64) != 1735689600 {
		t.Fatalf("exp mismatch")
	}
	ws, err := workspaceFromCredentials(map[string]string{"access_token": token})
	if err != nil || ws != "ws-9" {
		t.Fatalf("workspace = %q err=%v", ws, err)
	}
}

func TestTOTPPythonParity(t *testing.T) {
	cases := map[int64]string{59: "996554", 1111111109: "071271", 1234567890: "742275", 2000000000: "890699"}
	for at, want := range cases {
		got, err := totpAt("JBSWY3DPEHPK3PXP", at)
		if err != nil || got != want {
			t.Fatalf("totpAt(%d) = %q err=%v, want %q", at, got, err, want)
		}
	}
}

func TestFNV1aPythonParity(t *testing.T) {
	cases := map[string]string{
		"hello":      "888d766e",
		"seed123abc": "02fea6bd",
		"":           "ab3e7c0b",
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA": "9b265860",
		"gAAAAAB测试": "98319ab9",
	}
	for in, want := range cases {
		if got := fnv1a32(in); got != want {
			t.Fatalf("fnv1a32(%q) = %q, want %q", in, got, want)
		}
	}
}
