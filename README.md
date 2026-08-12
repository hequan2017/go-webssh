# go-webssh

一个简洁的 Web SSH 终端。服务端使用 Go 将浏览器 WebSocket 与目标主机 SSH 会话桥接，浏览器使用 xterm.js 显示交互式终端。

[![Go Version](https://img.shields.io/badge/Go-1.24.9-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

## 特性

- 浏览器内交互式 SSH 终端
- 密码或私钥认证
- 实时终端输入、输出和窗口尺寸同步
- 前端资源嵌入单一可执行文件
- 健康检查接口
- Docker 多阶段构建和非 root 运行

## 要求

- Go 1.24.9
- 可访问的 SSH 服务

## 快速开始

PowerShell：

```powershell
$env:SSH_HOST = "192.168.1.100"
$env:SSH_USER = "root"
$env:SSH_PASSWORD = "your_password"
go run .
```

打开 <http://localhost:8080>。

也可以使用私钥：

```powershell
$env:SSH_HOST = "192.168.1.100"
$env:SSH_USER = "root"
$env:SSH_KEY_PATH = "~/.ssh/id_ed25519"
go run .
```

设置 `SSH_KEY_PATH` 时优先使用私钥认证。

## 配置

| 环境变量 | 说明 | 默认值 |
| --- | --- | --- |
| `SSH_HOST` | 目标 SSH 主机 | `127.0.0.1` |
| `SSH_PORT` | 目标 SSH 端口 | `22` |
| `SSH_USER` | SSH 用户名 | `root` |
| `SSH_PASSWORD` | SSH 密码 | 空 |
| `SSH_KEY_PATH` | SSH 私钥路径 | 空 |
| `LISTEN_ADDR` | HTTP 监听地址 | `:8080` |

`SSH_PASSWORD` 与 `SSH_KEY_PATH` 至少配置一项。项目不会将凭据写入页面或日志。

## Docker

```powershell
docker build -t go-webssh .
docker run --rm -p 8080:8080 `
  -e SSH_HOST=192.168.1.100 `
  -e SSH_USER=root `
  -e SSH_PASSWORD=your_password `
  go-webssh
```

使用私钥时，以只读方式挂载密钥：

```powershell
docker run --rm -p 8080:8080 `
  -e SSH_HOST=192.168.1.100 `
  -e SSH_USER=root `
  -e SSH_KEY_PATH=/run/secrets/ssh_key `
  -v "${HOME}/.ssh/id_ed25519:/run/secrets/ssh_key:ro" `
  go-webssh
```

## 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/` | Web 终端页面 |
| `GET` | `/healthz` | 健康检查，返回 `{"status":"ok"}` |
| `GET` | `/ws/{id}?cols=120&rows=32` | WebSocket SSH 会话 |

WebSocket 输入支持原始终端数据，也支持以下 JSON 消息：

```json
{"type":"resize","cols":120,"rows":32}
```

```json
{"type":"cmd","cmd":"ls -la\n"}
```

## 开发

```powershell
go test ./...
go vet ./...
go build ./...
```

项目结构：

```text
.
├── main.go            # 启动、资源嵌入与优雅关闭
├── core/              # 配置、HTTP/WebSocket 和 SSH 桥接
├── web/html/          # 终端页面
├── static/            # xterm.js 及静态资源
└── Dockerfile
```

## 安全说明

- 项目按原有行为接受目标主机密钥，适合可信网络内的轻量部署；面向公网时应增加认证、TLS 和访问控制。
- WebSocket 仅接受同源浏览器请求。
- 不要将 SSH 密码或私钥提交到 Git 仓库。

## 贡献

欢迎提交 Issue 和 Pull Request。提交前请确保 `go test ./...`、`go vet ./...` 和 `go build ./...` 通过。

## 致谢

项目最初从 [dejavuzhou/felix](https://github.com/dejavuzhou/felix) 提取 WebSSH 模块并演进。

## License

[MIT](LICENSE)
