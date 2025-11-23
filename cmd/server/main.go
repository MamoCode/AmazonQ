package main

import (
	"fmt"
	"os"

	"amazonq-proxy/internal/api"

	"github.com/gin-gonic/gin"
)

// main 主服务入口函数，启动 Amazon Q 代理服务器
func main() {
	// 设置生产模式
	gin.SetMode(gin.ReleaseMode)

	// 启动 token 刷新器
	api.StartTokenRefresher()

	// 设置路由
	router := api.SetupRouter()

	// 获取端口配置
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	// 启动服务器
	addr := fmt.Sprintf("0.0.0.0:%s", port)
	fmt.Printf("Amazon Q Proxy Server starting on %s\n", addr)

	if err := router.Run(addr); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
}
