# 构建阶段
FROM golang:1.21-alpine AS builder

WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o amazonq-proxy cmd/server/main.go

# 运行阶段
FROM alpine:latest

# 配置 Alpine 镜像源并安装 ca-certificates（添加重试机制）
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk --no-cache add ca-certificates || \
    (sed -i 's/mirrors.aliyun.com/dl-cdn.alpinelinux.org/g' /etc/apk/repositories && \
     apk --no-cache add ca-certificates)

WORKDIR /root/

# 从构建阶段复制可执行文件
COPY --from=builder /app/amazonq-proxy .

# 暴露端口
EXPOSE 8000

# 运行应用
CMD ["./amazonq-proxy"]
