# GoChatX

基于 Go 语言实现的实时聊天系统，支持群聊、私聊、消息持久化和在线状态管理。

## 架构

```
┌─────────────┐     ┌──────────────┐     ┌───────────┐
│   Browser   │────▶│   Gateway    │────▶│ AuthSvc   │
│  (WebSocket)│     │  (:8080)     │     │ (:50051)  │
└─────────────┘     └──────┬───────┘     └─────┬─────┘
                           │                   │
                    ┌──────┼──────┐      ┌─────┴─────┐
                    ▼      ▼      ▼      ▼           ▼
                 ┌─────┐ ┌─────┐ ┌─────┐ ┌─────────┐
                 │Redis│ │Mongo│ │MySQL│ │JWT Auth │
                 └─────┘ └─────┘ └─────┘ └─────────┘
```

- **Gateway** — WebSocket 网关，处理实时消息路由、房间管理、心跳检测
- **AuthSvc** — gRPC 认证服务，提供注册、登录、Token 验证
- **Redis** — 在线状态、最后活跃时间
- **MongoDB** — 消息持久化，支持历史查询和离线消息推送
- **MySQL** — 用户账号存储

## 功能

- **群聊** — 创建/加入房间，房间内广播消息
- **私聊** — 点对点消息，在线实时送达，离线持久化
- **消息历史** — MongoDB 持久化，支持按房间查询历史记录
- **离线消息** — 用户上线自动推送未送达的私聊消息
- **在线状态** — Redis 实时跟踪，心跳保活
- **JWT 认证** — 注册/登录获取 Token，WebSocket 连接时验证
- **开发模式** — 无 AuthSvc 时自动降级为 token-as-userID 模式

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.25 |
| WebSocket | gorilla/websocket |
| gRPC | google.golang.org/grpc |
| JWT | golang-jwt/jwt |
| 数据库 | MySQL 8.0 + MongoDB 7 |
| 缓存 | Redis 7 |
| 密码加密 | bcrypt |
| 配置 | spf13/viper |

## 快速开始

### 1. 启动依赖服务

```bash
docker compose up -d
```

这会启动 MySQL（3306）、MongoDB（27017）、Redis（6379）。

### 2. 启动认证服务

```bash
go run ./cmd/authsvc
# Auth service listening on :50051
# 首次启动会自动创建 users 表
```

### 3. 启动网关

```bash
go run ./cmd/gateway
# Gateway listening on :8080
```

### 4. 打开前端

浏览器访问 `http://localhost:8080`，输入 UserID 点击 Connect 即可开始聊天。

## WebSocket 协议

连接地址：`ws://localhost:8080/ws`

### 客户端 → 服务端

| type | 说明 | 参数 |
|------|------|------|
| `auth` | 认证 | `token` — JWT token（或开发模式下直接填 userID） |
| `join` | 加入房间 | `room_id` — 房间 ID |
| `leave` | 离开房间 | `room_id` |
| `send` | 发送群聊 | `room_id`, `content` |
| `private` | 发送私聊 | `to` — 目标用户ID, `content` |
| `history` | 查询历史 | `room_id`, `limit`（可选） |
| `ping` | 心跳 | — |

### 服务端 → 客户端

| type | 说明 |
|------|------|
| `authed` | 认证成功，返回 `user` ID |
| `joined` | 加入房间成功，附带 `room` 和 `history` |
| `left` | 离开房间 |
| `message` | 群聊消息广播 |
| `private` | 私聊消息 |
| `history` | 历史消息查询结果 |
| `ack` | 私聊发送确认 |
| `pong` | 心跳响应 |
| `error` | 错误信息 |

## 配置

默认配置在 `config.yaml`，所有选项均可通过环境变量覆盖：

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `DB_DSN` | `root:123456@tcp(127.0.0.1:3306)/gochatx?parseTime=true` | MySQL DSN |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis 地址 |
| `JWT_SECRET` | `supersecretkey` | JWT 签名密钥 |
| `LISTEN_ADDR` | `:50051` | AuthSvc 监听地址 |

Gateway 通过 `config.yaml` 或 viper 默认值配置。

## 项目结构

```
GoChatX/
├── api/                       # gRPC proto 定义及生成代码
│   ├── auth.proto
│   ├── auth.pb.go
│   └── auth_grpc.pb.go
├── cmd/
│   ├── authsvc/main.go        # 认证服务入口
│   └── gateway/main.go        # 网关入口
├── internal/
│   ├── auth/service.go        # 认证逻辑（注册/登录/JWT）
│   ├── gateway/
│   │   ├── clients.go         # WebSocket 客户端管理
│   │   ├── room.go            # 房间管理
│   │   └── ws_handle.go       # WebSocket 消息处理
│   └── storage/mango.go       # MongoDB 存储层
├── web/index.html             # 前端 Demo 页面
├── config.yaml                # 配置文件
├── docker-compose.yml         # 依赖服务编排
├── go.mod
└── go.sum
```
