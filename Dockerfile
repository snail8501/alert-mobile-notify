FROM golang:1.24.7-alpine AS builder

# 设置工作目录
WORKDIR /go/src/app

# 设置 Go 模块代理（关键！）
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=off

# 复制 go.mod 并下载依赖
COPY go.mod ./
RUN --mount=type=cache,mode=0777,id=gomod,target=/go/pkg/mod \
    go mod download

# 复制项目文件
COPY . .

# 编译项目（此时 GOPROXY 依然生效）
RUN go mod tidy && go build -o alert-mobile-notify .

# =======================================
# ===== Final Stage =====
# =======================================
FROM golang:1.24.7-alpine

WORKDIR /app

COPY --from=builder /go/src/app/alert-mobile-notify ./
COPY --from=builder /go/src/app/config.yaml ./config.yaml

ENTRYPOINT ["/app/alert-mobile-notify"]