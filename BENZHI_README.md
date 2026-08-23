# BENZHI_README

## 项目说明
- 项目：benzhi-project-6bda51ed-7fcc-4c3d-aeff-2551ce3c0400
- 项目用途：博物馆展签事实核校与发布系统已实现从案卷建档、事实主张与来源证据维护、完整性预检、专业核校、文字复核、候选版本冻结到发布凭据签发及审计查询的完整流程。服务默认监听127.0.0.1:19081，支持地址参数、PORT和真实回环selfcheck。
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/server -addr=127.0.0.1:19081 -selfcheck
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-6bda51ed-7fcc-4c3d-aeff-2551ce3c0400-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-6bda51ed-7fcc-4c3d-aeff-2551ce3c0400-arm64 linux/arm64
docker run -it benzhi-project-6bda51ed-7fcc-4c3d-aeff-2551ce3c0400-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/server -addr=127.0.0.1:19081 -selfcheck`
