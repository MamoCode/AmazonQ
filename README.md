# Amazon Q Proxy

将 Amazon Q API 转换为 Claude API 格式的代理服务

## 支持的 API 端点

- `POST /v1/messages` - Claude Messages API 兼容端点
- 支持的模型: `claude-sonnet-4.5`, `claude-haiku-4.5`
- 在 `Cherry Studio`, 等客户端中使用时可能会因为apiKey格式问题导致无法传递正确apiKey, 建议配合其他轮询程序使用

## 快速开始

### 1. 安装依赖

```bash
cd go
go mod download
```

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env 文件配置端口等参数
```

### 3. 运行服务

```bash
# 开发模式
go run cmd/server/main.go

# 编译运行
go build -o amazonq-proxy cmd/server/main.go
./amazonq-proxy
```

### 4. 构建可执行文件

```bash
# Windows
go build -o amazonq-proxy.exe cmd/server/main.go

# Linux
GOOS=linux GOARCH=amd64 go build -o amazonq-proxy cmd/server/main.go

# macOS
GOOS=darwin GOARCH=amd64 go build -o amazonq-proxy cmd/server/main.go
```

## 使用方法

### 获取认证凭据

Token 格式：`clientId:clientSecret:refreshToken`

参考项目的 `auth` 文件夹 或 [点我](https://amazonq-auth.deno.dev/) 获取凭据。

### 调用 API

```bash
curl -X POST http://localhost:8000/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_CLIENT_ID:YOUR_CLIENT_SECRET:YOUR_REFRESH_TOKEN" \
  -d '{
    "model": "claude-sonnet-4.5",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ]
  }'
```

## 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| `PORT` | 服务器端口 | `8000` |
| `HTTP_PROXY` | HTTP 代理地址 | 无 |

## Docker 部署

### 使用 Docker Compose（推荐）

1. 克隆仓库
```bash
git clone https://github.com/MamoCode/AmazonQ
cd AmazonQ
```

2. 创建环境变量文件
```bash
cp .env.example .env
# 编辑 .env 文件配置端口等参数
```

3. 启动服务
```bash
cd docker
docker compose up -d
```

4. 查看日志
```bash
docker compose logs -f
```

5. 停止服务
```bash
docker compose down
```

### 使用 Docker 命令

1. 运行容器
```bash
docker run -d \
  --name amazonq-proxy \
  -p 8000:8000 \
  --env-file .env \
  ghcr.io/mamocode/amazonq:latest
```

2. 查看日志
```bash
docker logs -f amazonq-proxy
```

3. 停止容器
```bash
docker stop amazonq-proxy
docker rm amazonq-proxy
```

## 项目结构

```
.
├── cmd/
│   └── server/          # 主程序入口
├── internal/
│   ├── api/            # API 路由和处理器
│   ├── amazonq/        # Amazon Q 客户端
│   ├── config/         # 配置管理
│   ├── core/           # 核心转换逻辑
│   └── utils/          # 工具函数
├── auth/               # 认证工具
│   ├── generate_auth_url.go  # 生成授权链接
│   └── extract_token.go      # 提取令牌
├── docker/             # Docker 配置
│   ├── Dockerfile
│   └── docker-compose.yml
└── .github/
    └── workflows/      # CI/CD 配置
        ├── release.yml           # 发布工作流
        └── docker-build-push.yml # Docker 构建推送
