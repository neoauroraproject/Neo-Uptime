// Package auth holds the credential primitives: password hashing, random
// tokens, TOTP and a login rate limiter.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 is defined over HMAC-SHA1
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword returns a bcrypt hash.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(b), err
}

// CheckPassword compares a password with its hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash is compared against when a username does not exist, so a wrong
// username costs as long as a wrong password and the two cannot be told apart.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("neo-uptime-dummy"), 12)

// BurnTime spends the same time a real password check would.
func BurnTime(pw string) { _ = bcrypt.CompareHashAndPassword(dummyHash, []byte(pw)) }

// ValidatePassword enforces a minimal policy.
func ValidatePassword(pw string) error {
	if len(pw) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(pw) > 72 {
		return fmt.Errorf("password must be at most 72 bytes")
	}
	return nil
}

// RandomToken returns n random bytes, base64url-encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RandomAlnum returns a random string of letters and digits.
func RandomAlnum(n int) string {
	const chars = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// HashToken returns the hex SHA-256 of a secret, which is what gets stored.
func HashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

// ---------- TOTP (RFC 6238) ----------

// NewTOTPSecret returns a fresh base32 secret.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b), "=")
}

// TOTPURI is the otpauth:// link authenticator apps read from a QR code.
func TOTPURI(issuer, account, secret string) string {
	v := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

func totpAt(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", code), nil
}

// VerifyTOTP accepts the current code and one step either side for clock drift.
func VerifyTOTP(secret, code string) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return false
	}
	step := uint64(time.Now().Unix() / 30)
	for _, d := range []int64{0, -1, 1} {
		want, err := totpAt(secret, uint64(int64(step)+d))
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// ---------- rate limiting ----------

// Limiter locks an address out after repeated failures.
type Limiter struct {
	mu    sync.Mutex
	fails map[string]*entry
	Max   int
	Lock  time.Duration
}

type entry struct {
	count int
	until time.Time
	last  time.Time
}

// NewLimiter allows max failures before locking for lock.
func NewLimiter(max int, lock time.Duration) *Limiter {
	return &Limiter{fails: map[string]*entry{}, Max: max, Lock: lock}
}

// Blocked reports whether ip is locked out and for how long.
func (l *Limiter) Blocked(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.fails[ip]
	if !ok {
		return false, 0
	}
	if left := time.Until(e.until); left > 0 {
		return true, left
	}
	return false, 0
}

// Fail records a failure.
func (l *Limiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, e := range l.fails {
		if now.Sub(e.last) > time.Hour {
			delete(l.fails, k)
		}
	}
	e, ok := l.fails[ip]
	if !ok {
		e = &entry{}
		l.fails[ip] = e
	}
	e.count++
	e.last = now
	if e.count >= l.Max {
		e.until = now.Add(l.Lock)
		e.count = 0
	}
}

// Succeed clears an address.
func (l *Limiter) Succeed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}
