package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/logger"
)

type Claims struct {
	jwt.RegisteredClaims
	TwoFAVerified bool `json:"2fa_verified"`
}

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header"})
			return
		}

		tokenString := parts[1]
		cfg := config.Get()

		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(cfg.JWTSecret), nil
		})

		if err != nil {
			logger.Debug("JWT parse error: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		claims, ok := token.Claims.(*Claims)
		if !ok || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Set("user_id", claims.Subject)
		c.Set("jti", claims.ID)
		c.Set("2fa_verified", claims.TwoFAVerified)
		c.Next()
	}
}

func ServiceKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header"})
			return
		}

		key := parts[1]
		cfg := config.Get()

		keyHash := sha256.Sum256([]byte(key))
		hashes := serviceKeyHashes(cfg.ServiceKeys)
		valid := 0
		for i := range hashes {
			valid |= subtle.ConstantTimeCompare(keyHash[:], hashes[i][:])
		}

		if valid != 1 {
			logger.Warn("Invalid service key attempt from %s", c.ClientIP())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid service key"})
			return
		}
		logger.Debug("Service key authenticated")

		c.Set("service_key_auth", true)
		c.Next()
	}
}

var (
	serviceKeyMu    sync.Mutex
	serviceKeyCache []string
	serviceKeySums  [][32]byte
)

// serviceKeyHashes returns SHA-256 of the configured keys, recomputed only when the set changes.
// The presented key is still hashed on every request and compared in constant time.
func serviceKeyHashes(keys []string) [][32]byte {
	serviceKeyMu.Lock()
	defer serviceKeyMu.Unlock()
	if sameStrings(serviceKeyCache, keys) {
		return serviceKeySums
	}
	sums := make([][32]byte, len(keys))
	for i, k := range keys {
		sums[i] = sha256.Sum256([]byte(k))
	}
	serviceKeyCache = append([]string(nil), keys...)
	serviceKeySums = sums
	return serviceKeySums
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
