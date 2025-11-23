package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"amazonq-proxy/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TokenCache 存储用户的 Token 缓存信息
type TokenCache struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	ClientSecret string
	LastRefresh  time.Time
}

var (
	// tokenMap Token 缓存映射
	tokenMap = make(map[string]*TokenCache)
	// tokenMutex Token 缓存互斥锁
	tokenMutex sync.RWMutex
)

// sha256Hash 计算输入文本的 SHA256 哈希值
// 参数 text 为待哈希的文本
// 返回十六进制编码的哈希字符串
func sha256Hash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

// parseBearerToken 解析 Bearer token 格式: clientId:clientSecret:refreshToken
// 参数 bearerToken 为完整的 token 字符串
// 返回 clientId, clientSecret, refreshToken
func parseBearerToken(bearerToken string) (string, string, string) {
	parts := strings.SplitN(bearerToken, ":", 3)
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// handleTokenRefresh 使用 refresh token 获取新的 access token
// 参数 clientID 为客户端 ID
// 参数 clientSecret 为客户端密钥
// 参数 refreshToken 为刷新令牌
// 返回新的 access token 和可能的错误
func handleTokenRefresh(clientID, clientSecret, refreshToken string) (string, error) {
	payload := map[string]string{
		"grantType":    "refresh_token",
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"refreshToken": refreshToken,
	}

	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", config.TokenURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}

	// 设置 OIDC 请求头
	for k, v := range config.OIDCHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token refresh failed: %d - %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	accessToken, ok := result["accessToken"].(string)
	if !ok {
		return "", fmt.Errorf("no access token in response")
	}

	return accessToken, nil
}

// AuthMiddleware 认证中间件，支持 OpenAI Bearer token 和 Claude x-api-key 两种格式
// 验证通过后会将 access token 等信息存入上下文
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 优先使用 x-api-key（Claude 格式）
		token := c.GetHeader("x-api-key")

		// 如果没有 x-api-key，尝试从 Authorization header 获取（OpenAI 格式）
		if token == "" {
			auth := c.GetHeader("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Missing authentication. Provide Authorization header or x-api-key",
			})
			c.Abort()
			return
		}

		tokenHash := sha256Hash(token)

		// 检查缓存
		tokenMutex.RLock()
		cached, exists := tokenMap[tokenHash]
		tokenMutex.RUnlock()

		if exists {
			c.Set("accessToken", cached.AccessToken)
			c.Set("clientId", cached.ClientID)
			c.Set("clientSecret", cached.ClientSecret)
			c.Set("refreshToken", cached.RefreshToken)
			c.Next()
			return
		}

		// 解析 token
		clientID, clientSecret, refreshToken := parseBearerToken(token)
		if clientID == "" || clientSecret == "" || refreshToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid token format. Expected: clientId:clientSecret:refreshToken",
			})
			c.Abort()
			return
		}

		// 刷新 token
		accessToken, err := handleTokenRefresh(clientID, clientSecret, refreshToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": fmt.Sprintf("Failed to refresh access token: %v", err),
			})
			c.Abort()
			return
		}

		// 缓存
		tokenMutex.Lock()
		tokenMap[tokenHash] = &TokenCache{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			LastRefresh:  time.Now(),
		}
		tokenMutex.Unlock()

		c.Set("accessToken", accessToken)
		c.Set("clientId", clientID)
		c.Set("clientSecret", clientSecret)
		c.Set("refreshToken", refreshToken)
		c.Next()
	}
}

// RefreshAllTokens 全局刷新器，遍历并刷新所有缓存的 token
// 刷新失败的 token 会从缓存中移除
func RefreshAllTokens() {
	tokenMutex.RLock()
	count := len(tokenMap)
	tokenMutex.RUnlock()

	if count == 0 {
		return
	}

	fmt.Printf("[Token Refresher] Starting token refresh cycle...\n")
	refreshCount := 0

	tokenMutex.RLock()
	tokens := make(map[string]*TokenCache)
	for k, v := range tokenMap {
		tokens[k] = v
	}
	tokenMutex.RUnlock()

	for hash, cache := range tokens {
		newToken, err := handleTokenRefresh(cache.ClientID, cache.ClientSecret, cache.RefreshToken)
		if err != nil {
			fmt.Printf("[Token Refresher] Failed to refresh token for hash: %s...: %v, removing from cache\n", hash[:8], err)
			tokenMutex.Lock()
			delete(tokenMap, hash)
			tokenMutex.Unlock()
			continue
		}

		tokenMutex.Lock()
		if tokenMap[hash] != nil {
			tokenMap[hash].AccessToken = newToken
			tokenMap[hash].LastRefresh = time.Now()
		}
		tokenMutex.Unlock()

		refreshCount++
	}

	fmt.Printf("[Token Refresher] Refreshed %d/%d tokens\n", refreshCount, count)
}

// StartTokenRefresher 启动定时 token 刷新器
// 在后台 goroutine 中每 45 分钟自动刷新所有缓存的 token
func StartTokenRefresher() {
	go func() {
		ticker := time.NewTicker(45 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			RefreshAllTokens()
		}
	}()
}
