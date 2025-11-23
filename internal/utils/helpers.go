package utils

import (
	"net/http"
	"net/url"
	"os"
	"time"
)

// GetProxy 从环境变量获取代理配置
// 优先使用 HTTP_PROXY，其次使用 HTTPS_PROXY
// 返回代理 URL 字符串，如果未配置则返回空字符串
func GetProxy() string {
	proxy := os.Getenv("HTTP_PROXY")
	if proxy == "" {
		proxy = os.Getenv("HTTPS_PROXY")
	}
	return proxy
}

// CreateProxyTransport 创建配置了代理的 HTTP Transport
// 如果环境变量中配置了代理则使用代理，否则返回默认配置的 Transport
// 返回配置完成的 HTTP Transport 实例
func CreateProxyTransport() *http.Transport {
	proxy := GetProxy()
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return transport
}
