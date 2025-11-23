package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// TokenResponse 令牌响应结构
type TokenResponse struct {
	RefreshToken string `json:"refreshToken"`
	AccessToken  string `json:"accessToken"`
}

// loadConfig 从文件加载授权配置
// 读取 auth/auth_config.txt 文件中的配置项
// 返回配置映射和可能的错误
func loadConfig() (map[string]string, error) {
	config := make(map[string]string)

	file, err := os.Open("auth/auth_config.txt")
	if err != nil {
		return nil, fmt.Errorf("错误: 未找到 auth/auth_config.txt 文件\n请先运行 go run auth/generate_auth_url.go 生成授权链接")
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				config[parts[0]] = parts[1]
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return config, nil
}

// exchangeToken 使用授权码交换访问令牌和刷新令牌
// 参数 code 为 OAuth2 授权码
// 参数 config 为授权配置映射
// 返回令牌响应和可能的错误
func exchangeToken(code string, config map[string]string) (*TokenResponse, error) {
	payload := map[string]interface{}{
		"grantType":    "authorization_code",
		"code":         code,
		"redirectUri":  config["redirect_uri"],
		"clientId":     config["client_id"],
		"codeVerifier": config["code_verifier"],
	}

	if config["client_secret"] != "" {
		payload["clientSecret"] = config["client_secret"]
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(
		fmt.Sprintf("https://oidc.%s.amazonaws.com/token", config["region"]),
		"application/json",
		strings.NewReader(string(jsonData)),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("令牌交换失败 (%d): %s", resp.StatusCode, string(body))
	}

	var tokens TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}

	if tokens.RefreshToken == "" {
		return nil, fmt.Errorf("未获取到 refresh_token")
	}

	return &tokens, nil
}

// extractAndExchangeToken 从回调 URL 提取授权码并交换令牌
// 参数 callbackURL 为 OAuth2 回调 URL
// 执行完整的令牌提取、验证和交换流程，并保存凭证到文件
// 返回可能的错误
func extractAndExchangeToken(callbackURL string) error {
	// 加载配置
	config, err := loadConfig()
	if err != nil {
		return err
	}

	// 解析回调 URL
	parsedURL, err := url.Parse(callbackURL)
	if err != nil {
		return fmt.Errorf("解析 URL 失败: %v", err)
	}

	queryParams := parsedURL.Query()
	code := queryParams.Get("code")
	returnedState := queryParams.Get("state")

	if code == "" {
		return fmt.Errorf("错误: 回调 URL 中未找到授权码\n请确保复制了完整的跳转链接")
	}

	fmt.Println("\n✓ 成功提取授权码")

	// 验证 state
	if returnedState != config["state"] {
		fmt.Println("警告: State 验证失败,但继续尝试交换令牌...")
	}

	// 交换令牌
	fmt.Println("\n正在交换令牌...")
	tokens, err := exchangeToken(code, config)
	if err != nil {
		return err
	}

	// 格式化输出
	credentials := fmt.Sprintf("%s:%s:%s",
		config["client_id"],
		config["client_secret"],
		tokens.RefreshToken,
	)

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("成功获取令牌!")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("\nClient ID: %s\n", config["client_id"])
	if config["client_secret"] != "" {
		fmt.Printf("Client Secret: %s\n", config["client_secret"])
	} else {
		fmt.Println("Client Secret: (无)")
	}
	fmt.Printf("Refresh Token: %s\n", tokens.RefreshToken)
	fmt.Println("\n完整凭证 (格式: client_id:client_secret:refresh_token):")
	fmt.Println(credentials)
	fmt.Println(strings.Repeat("=", 80))

	// 保存到文件
	if err := os.WriteFile("auth/amazonq_credentials.txt", []byte(credentials), 0644); err != nil {
		return err
	}

	fmt.Println("\n✓ 凭证已保存到 auth/amazonq_credentials.txt")

	return nil
}

func main() {
	fmt.Println("请粘贴授权后跳转的完整 URL:")
	reader := bufio.NewReader(os.Stdin)
	callbackURL, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取输入失败: %v\n", err)
		os.Exit(1)
	}

	callbackURL = strings.TrimSpace(callbackURL)
	if callbackURL == "" {
		fmt.Println("错误: URL 不能为空")
		os.Exit(1)
	}

	if err := extractAndExchangeToken(callbackURL); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
