package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// Region AWS 区域
	Region = "us-east-1"
	// ClientName OAuth2 客户端名称
	ClientName = "AWS IDE Extensions for VSCode"
	// StartURL OIDC 服务起始 URL
	StartURL = "https://view.awsapps.com/start"
	// Scopes OAuth2 请求的权限范围
	Scopes = "codewhisperer:completions,codewhisperer:analysis,codewhisperer:conversations,codewhisperer:transformations,codewhisperer:taskassist"
)

// ClientRegistration 客户端注册响应结构
type ClientRegistration struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

// AuthConfig 授权配置结构，用于存储认证流程中的参数
type AuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	CodeVerifier string
	State        string
	Region       string
}

// base64URLEncode 执行 Base64 URL 安全编码
// 参数 data 为待编码的字节数据
// 返回 URL 安全的 Base64 编码字符串
func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// generateRandomString 生成指定长度的随机字符串
// 参数 length 为生成的随机字节长度
// 返回 Base64 URL 编码的随机字符串和可能的错误
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

// generateCodeChallenge 生成 PKCE code_challenge
// 参数 verifier 为 code_verifier 字符串
// 返回 SHA256 哈希后的 Base64 URL 编码字符串
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64URLEncode(hash[:])
}

// registerClient 向 AWS OIDC 服务注册客户端
// 参数 redirectURI 为 OAuth2 回调地址
// 返回客户端注册信息和可能的错误
func registerClient(redirectURI string) (*ClientRegistration, error) {
	requestBody := map[string]interface{}{
		"clientName":   ClientName,
		"clientType":   "public",
		"grantTypes":   []string{"authorization_code", "refresh_token"},
		"redirectUris": []string{redirectURI},
		"scopes":       strings.Split(Scopes, ","),
		"issuerUrl":    StartURL,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(
		fmt.Sprintf("https://oidc.%s.amazonaws.com/client/register", Region),
		"application/json",
		strings.NewReader(string(jsonData)),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("客户端注册失败: %d, %s", resp.StatusCode, string(body))
	}

	var registration ClientRegistration
	if err := json.NewDecoder(resp.Body).Decode(&registration); err != nil {
		return nil, err
	}

	return &registration, nil
}

// saveConfig 保存授权配置到文件
// 参数 config 为授权配置结构
// 返回可能的错误
func saveConfig(config *AuthConfig) error {
	file, err := os.Create("auth/auth_config.txt")
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintf(file, "client_id=%s\n", config.ClientID)
	fmt.Fprintf(file, "client_secret=%s\n", config.ClientSecret)
	fmt.Fprintf(file, "redirect_uri=%s\n", config.RedirectURI)
	fmt.Fprintf(file, "code_verifier=%s\n", config.CodeVerifier)
	fmt.Fprintf(file, "state=%s\n", config.State)
	fmt.Fprintf(file, "region=%s\n", config.Region)

	return nil
}

// generateAuthURL 生成 OAuth2 授权链接并保存配置
// 执行完整的客户端注册和授权 URL 生成流程
// 返回可能的错误
func generateAuthURL() error {
	// 使用随机端口
	mathrand.Seed(time.Now().UnixNano())
	callbackPort := mathrand.Intn(55000) + 10000
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", callbackPort)

	fmt.Println("正在注册客户端...")
	registration, err := registerClient(redirectURI)
	if err != nil {
		return err
	}

	// 生成 PKCE 参数
	codeVerifier, err := generateRandomString(64)
	if err != nil {
		return err
	}
	state := uuid.New().String()
	codeChallenge := generateCodeChallenge(codeVerifier)

	// 构造授权 URL
	params := url.Values{}
	params.Add("response_type", "code")
	params.Add("client_id", registration.ClientID)
	params.Add("redirect_uri", redirectURI)
	params.Add("scopes", Scopes)
	params.Add("state", state)
	params.Add("code_challenge", codeChallenge)
	params.Add("code_challenge_method", "S256")

	authURL := fmt.Sprintf("https://oidc.%s.amazonaws.com/authorize?%s", Region, params.Encode())

	// 保存配置
	config := &AuthConfig{
		ClientID:     registration.ClientID,
		ClientSecret: registration.ClientSecret,
		RedirectURI:  redirectURI,
		CodeVerifier: codeVerifier,
		State:        state,
		Region:       Region,
	}

	if err := saveConfig(config); err != nil {
		return err
	}

	// 输出信息
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("授权链接已生成!")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("\n授权链接:\n%s\n\n", authURL)
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("\n请按以下步骤操作:")
	fmt.Println("1. 复制上面的授权链接到浏览器打开")
	fmt.Println("2. 使用 Google 账号登录并授权")
	fmt.Println("3. 授权后会跳转到一个无法访问的页面(这是正常的)")
	fmt.Println("4. 复制浏览器地址栏中完整的跳转链接")
	fmt.Println("5. 运行 go run auth/extract_token.go 并粘贴跳转链接")
	fmt.Println(strings.Repeat("=", 80))

	return nil
}

func main() {
	if err := generateAuthURL(); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
