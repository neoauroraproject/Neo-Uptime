package auth

import (
	"testing"
	"time"
)

func TestTOTPRoundTrip(t *testing.T) {
	secret := NewTOTPSecret()
	code, err := totpAt(secret, uint64(time.Now().Unix()/30))
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyTOTP(secret, code) {
		t.Fatal("current code rejected")
	}
	if VerifyTOTP(secret, "000000") && code != "000000" {
		t.Fatal("wrong code accepted")
	}
}

// RFC 6238 appendix B, SHA-1, T = 59s → 94287082 (last six digits 287082).
func TestTOTPVector(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // "12345678901234567890"
	got, err := totpAt(secret, 59/30)
	if err != nil {
		t.Fatal(err)
	}
	if got != "287082" {
		t.Fatalf("got %s, want 287082", got)
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") || CheckPassword(h, "wrong") {
		t.Fatal("password check is wrong")
	}
	if ValidatePassword("short") == nil {
		t.Fatal("short password accepted")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		l.Fail("1.2.3.4")
	}
	if blocked, _ := l.Blocked("1.2.3.4"); !blocked {
		t.Fatal("not blocked after max failures")
	}
	if blocked, _ := l.Blocked("5.6.7.8"); blocked {
		t.Fatal("unrelated address blocked")
	}
}
