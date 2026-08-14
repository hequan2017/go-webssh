# go-webssh Bastion

一个面向单节点部署的轻量跳板机。使用 Go 提供身份认证、RBAC、SSH/SFTP 代理、凭据加密、操作审计和终端会话记录，前端资源嵌入单一可执行文件。

[![Go Version](https://img.shields.io/badge/Go-1.24.9-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![GitHub Pages](https://github.com/hequan2017/go-webssh/actions/workflows/pages.yml/badge.svg?branch=main)](https://github.com/hequan2017/go-webssh/actions/workflows/pages.yml)

[在线演示](https://hequan2017.github.io/go-webssh/)为只读静态控制台，不连接 SSH，也不会收集凭据。

## 界面预览

| 概览（深色） | 概览（浅色） |
| --- | --- |
| ![概览-深色](static/demo/dashboard-dark.png) | ![概览-浅色](static/demo/dashboard-light.png) |

| 资产管理 | 用户与权限 |
| --- | --- |
| ![资产管理](static/demo/assets-light.png) | ![用户与权限](static/demo/users-light.png) |

![会话记录](static/demo/sessions-light.png)

## 功能

- 本地账号登录，HttpOnly/SameSite 会话 Cookie，登录失败限流
- 三类角色：管理员、运维人员、审计员
- 用户生命周期管理：随机初始密码开账号、最后登录时间、删除用户并即时吊销会话
- 基于资产组的服务器访问授权
- 多服务器资产管理和启用/禁用
- 资产 SSH 握手与认证连通性测试（不执行远程命令）
- 网段自动发现：CIDR 并发探测开放 SSH 端口的设备并一键导入资产
- 密码、普通私钥和带口令私钥登录；密码认证自动回退键盘交互（兼容交换机等设备）
- 跳板机级联（ProxyJump）：资产可配置经另一资产中转，最多 5 层，终端与文件传输同链路
- AES-256-GCM 凭据加密，API 永不返回明文
- 可选 SSH `SHA256:` 主机密钥指纹校验
- 浏览器交互式终端（显示完整跳板路径）、窗口同步、心跳保活、重连、清屏和全屏
- 明暗主题切换，跟随系统偏好并持久化
- SFTP 目录浏览、文件上传与下载；上传默认不覆盖同名文件
- 登录、配置变更、SSH 命令、会话和文件传输审计（含跳板链路）
- SSH 输入/输出会话录像在线回放与下载
- 管理员查看并强制断开活动 SSH 会话
- 用户自助修改密码，修改后旧登录会话立即失效
- Docker 非 root 运行、健康检查和数据卷
- 推送 `main` 后由 GitHub Actions 自动测试、构建并发布 Pages 演示

## 权限模型

| 角色 | 权限 |
| --- | --- |
| `admin` | 管理用户、凭据和资产；访问全部资产；查看审计和录像 |
| `operator` | 仅访问用户被授权的资产组；使用终端和 SFTP |
| `auditor` | 只读查看资产、审计日志、会话和录像；不能建立 SSH/SFTP 会话 |

运维人员的资产组支持精确名称，例如 `prod,test`；使用 `*` 表示全部资产组。

## 本地启动

首次启动必须指定至少 12 位的管理员密码：

```powershell
$env:BASTION_ADMIN_PASSWORD = "replace-with-a-strong-password"
go run .
```

打开 <http://127.0.0.1:8080>，使用用户名 `admin` 和设置的密码登录。首次启动会创建：

```text
data/
├── master.key       # 自动生成的凭据主密钥
├── state.json       # 用户、资产、加密凭据和会话索引
├── audit.jsonl      # 追加式审计日志
└── recordings/      # SSH 输入/输出录像
```

`data/` 已被 Git 忽略。不要将其中任何文件提交到仓库。

## 配置

| 环境变量 | 说明 | 默认值 |
| --- | --- | --- |
| `LISTEN_ADDR` | HTTP 监听地址 | `:8080` |
| `BASTION_DATA_DIR` | 状态、主密钥、审计和录像目录 | `data` |
| `BASTION_ADMIN_USER` | 首次启动管理员用户名 | `admin` |
| `BASTION_ADMIN_PASSWORD` | 首次启动管理员密码，至少 12 位 | 无 |
| `BASTION_MASTER_KEY` | Base64 编码的 32 字节主密钥；未设置时自动生成文件 | 无 |
| `BASTION_SESSION_HOURS` | Web 登录会话有效小时数 | `12` |
| `BASTION_MAX_UPLOAD_MB` | 单文件上传限制 | `100` |

管理员创建完成后，后续启动不再读取 `BASTION_ADMIN_PASSWORD` 修改密码。请在控制台中修改用户密码。

生成外部主密钥示例：

```powershell
$bytes = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
[Convert]::ToBase64String($bytes)
```

## Docker

```powershell
docker build -t go-webssh-bastion .
docker run --rm -p 8080:8080 `
  -e BASTION_ADMIN_PASSWORD=replace-with-a-strong-password `
  -v go-webssh-data:/data `
  go-webssh-bastion
```

容器以 UID/GID `65532` 运行，状态目录固定为 `/data`。

也可以使用 Compose：

```powershell
Copy-Item ".env.example" ".env"
# 修改 .env 中的管理员密码
docker compose up -d --build
```

Compose 默认启用只读根文件系统、删除全部 Linux capabilities、禁止提权，并将持久数据放在命名卷 `bastion-data`。`.env` 已被 Git 忽略，`.env.example` 只包含占位配置。

## 资产与凭据

1. 管理员在“凭据管理”创建密码或 SSH 私钥凭据。
2. 在“资产管理”填写主机、端口、SSH 用户、资产组并关联凭据；也可使用“网段发现”扫描 `192.168.1.0/24` 等 CIDR 后一键导入。
3. 内网设备可在“跳板机”字段选择另一资产作为中转（最多级联 5 层）；终端、文件传输和连通性测试都会先登录跳板机再转发。
4. 建议填写目标服务器的 `SHA256:` 主机密钥指纹（跳板机与目标各自独立校验）。
5. 为运维用户配置允许访问的资产组；连接链路上的每一跳都要求用户对其资产组有权限。

获取 OpenSSH 主机密钥指纹示例：

```powershell
ssh-keyscan example.com | ssh-keygen -lf - -E sha256
```

未配置主机密钥指纹时会兼容旧版行为并接受目标主机提供的密钥，仅建议在可信网络内使用。

## 文件传输

资产列表中的“文件”入口使用与终端相同的资产凭据建立独立 SFTP 会话：

- 支持浏览远程目录
- 支持选择多个文件并逐个上传
- 支持下载普通文件
- 支持新建目录、重命名、删除文件和空目录
- 上传使用排他创建，不覆盖远程同名文件
- 超出 `BASTION_MAX_UPLOAD_MB` 的不完整远程文件会被清理
- 浏览、上传和下载均写入审计日志

## 审计与会话记录

- `audit.jsonl` 记录认证、用户/资产/凭据变更、SSH 命令和文件操作，SSH 与文件事件包含完整跳板链路（`jump_path`）。
- `recordings/*.jsonl` 以 Base64 帧保存终端输入和输出。
- 录像只允许管理员和审计员下载。
- 审计文件可能包含用户在终端输入的命令和敏感输出，应按敏感数据保护。

## 备份与恢复

可恢复备份必须同时包含整个 `BASTION_DATA_DIR`，尤其是 `master.key` 与 `state.json`。只有 `state.json` 而没有对应主密钥时，加密凭据无法恢复。

建议在停止写入或冻结数据卷后备份：

```text
data/master.key
data/state.json
data/audit.jsonl
data/recordings/
```

恢复时将这些文件放回同一路径并保持服务账号可读写，然后使用原配置启动。

## HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/auth/login` | 登录 |
| `POST` | `/api/auth/logout` | 退出 |
| `GET` | `/api/me` | 当前用户 |
| `POST` | `/api/me/password` | 修改当前用户密码并注销旧会话 |
| `GET/POST/PUT/DELETE` | `/api/assets` | 资产管理（含 `jump_asset_id` 跳板级联字段） |
| `POST` | `/api/assets/{id}/test` | 测试 SSH 握手与认证 |
| `GET/POST/PUT/DELETE` | `/api/credentials` | 凭据管理 |
| `GET/POST/PUT` | `/api/users` | 用户与权限管理 |
| `DELETE` | `/api/users/{id}` | 删除用户并吊销其会话（不允许删除最后一个管理员） |
| `POST` | `/api/discovery` | 管理员网段发现：`{"cidr":"192.168.1.0/24","port":22,"timeout_ms":1500}`，返回开放 SSH 端口的主机与版本横幅 |
| `GET` | `/ws/{assetID}` | 已授权资产的 SSH WebSocket |
| `GET/POST` | `/api/assets/{id}/files` | SFTP 列表/上传 |
| `PATCH/DELETE` | `/api/assets/{id}/files` | SFTP 重命名/删除 |
| `POST` | `/api/assets/{id}/directories` | 新建远程目录 |
| `GET` | `/api/assets/{id}/download` | SFTP 下载 |
| `GET` | `/api/audits` | 审计日志 |
| `GET` | `/api/sessions` | 会话记录 |
| `DELETE` | `/api/sessions/{id}` | 管理员强制断开活动会话 |
| `GET` | `/api/sessions/{id}/recording` | 下载会话录像 |
| `GET` | `/healthz` | 健康检查 |

## GitHub Pages 一键部署

仓库内置 [`.github/workflows/pages.yml`](.github/workflows/pages.yml)。推送到 `main` 后自动执行：

1. `go test ./...`
2. `go vet ./...`
3. 构建 Go 可执行文件
4. 构建只读静态演示站
5. 发布到 GitHub Pages

首次使用时，在仓库 **Settings → Pages → Build and deployment → Source** 中选择 **GitHub Actions**。

GitHub Pages 不能运行 Go、SSH 或 SFTP 服务，Pages 版本只展示示例资产和管理界面。

## 安全边界

- 面向公网部署时必须在可信反向代理后启用 HTTPS、访问控制和请求限速。
- 应配置 SSH 主机密钥指纹，防止中间人攻击。
- 主密钥、状态、审计和录像均属于敏感数据，应限制文件权限并纳入安全备份。
- 当前版本为单节点文件持久化，不提供集群一致性、外部 SSO、MFA 或高可用；这些属于企业级扩展，不应把单节点部署描述为 HA。
- 禁用用户会立即清除其 Web 登录会话，但已建立的 SSH 会话应由管理员结合会话审计处置。

## 开发

```powershell
go test ./...
go vet ./...
go build ./...
node --check static/js/app.js
```

## License

[MIT](LICENSE)
