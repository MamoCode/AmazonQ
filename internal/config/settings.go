package config

// AmazonQAPIURL Amazon Q API 服务端点地址
const AmazonQAPIURL = "https://q.us-east-1.amazonaws.com/"

// OIDCBaseURL OIDC 认证服务基础 URL
const OIDCBaseURL = "https://oidc.us-east-1.amazonaws.com"

// TokenURL OIDC Token 获取端点
var TokenURL = OIDCBaseURL + "/token"

// DefaultHeaders Amazon Q API 默认请求头
var DefaultHeaders = map[string]string{
	"content-type":                "application/x-amz-json-1.0",
	"x-amz-target":                "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
	"user-agent":                  "aws-sdk-rust/1.3.9 ua/2.1 api/codewhispererstreaming/0.1.11582 os/windows lang/rust/1.87.0 md/appVersion-1.19.4 app/AmazonQ-For-CLI",
	"x-amz-user-agent":            "aws-sdk-rust/1.3.9 ua/2.1 api/codewhispererstreaming/0.1.11582 os/windows lang/rust/1.87.0 m/F app/AmazonQ-For-CLI",
	"x-amzn-codewhisperer-optout": "false",
	"amz-sdk-request":             "attempt=1; max=3",
}

// OIDCHeaders OIDC 认证请求头
var OIDCHeaders = map[string]string{
	"content-type":   "application/json",
	"user-agent":     "aws-sdk-rust/1.3.9 os/windows lang/rust/1.87.0",
	"x-amz-user-agent": "aws-sdk-rust/1.3.9 ua/2.1 api/ssooidc/1.88.0 os/windows lang/rust/1.87.0 m/E app/AmazonQ-For-CLI",
	"amz-sdk-request": "attempt=1; max=3",
}
