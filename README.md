# Amazon Q Proxy

将 Amazon Q 转换为 Claude API 格式的代理服务

## 项目结构

```
amazonq-proxy/
├── src/                    # Python 主服务
│   ├── api/               # API 服务层
│   ├── core/              # 核心功能（类型定义、格式转换）
│   ├── amazonq/           # Amazon Q 客户端模块
│   ├── config/            # 配置管理
│   └── utils/             # 工具函数
├── auth/                   # 授权服务（原始版本）
├── app.py                 # 主服务入口
└── requirements.txt        # Python 依赖
```

## 快速开始

### 本地运行

```bash
pip install -r requirements.txt
python app.py
```

默认端口: 8000

### Docker 运行

**使用 Docker Compose（推荐）：**
```bash
# 启动服务
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down
```

**使用预构建镜像：**
```bash
# 使用 docker/docker-compose.yml
cd docker
docker-compose up -d
```

**手动构建：**
```bash
docker build -t amazonq-proxy .
docker run -p 8000:8000 amazonq-proxy
```

## 使用方法

### 获取认证信息

Token 格式: `clientId:clientSecret:refreshToken`

### API 调用
**使用 Authorization Bearer / x-api-key:**
```bash
curl -X POST http://localhost:8000/v1/messages \
  -H "x-api-key: clientId:clientSecret:refreshToken" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.5",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

## 环境变量

- `PORT`: 服务端口 (默认: 8000)
- `HTTP_PROXY`: HTTP/HTTPS 代理地址 (可选，格式: `http://host:port`)