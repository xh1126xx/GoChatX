# GoChatX

基于 Go 语言实现的实时聊天系统，支持群聊、私聊、消息持久化和在线状态管理。提供 REST API 注册登录 + WebSocket 实时通信，开箱即用。

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

- **Gateway** — HTTP REST API + WebSocket 网关，处理注册登录、实时消息路由、房间管理、在线用户
- **AuthSvc** — gRPC 认证服务，提供注册、登录、Token 验证，bcrypt 密码存储
- **Redis** — 在线状态、最后活跃时间
- **MongoDB** — 消息持久化，支持历史查询和离线消息推送
- **MySQL** — 用户账号持久化

## 功能

- **用户系统** — REST API 注册/登录，JWT Token 认证，bcrypt 密码加密
- **群聊** — 创建/加入房间，房间内广播消息，自动加载历史
- **私聊** — 点击在线用户发起私聊，在线实时送达，离线持久化
- **消息历史** — MongoDB 持久化，支持索引优化查询，加入房间自动加载最近 50 条
- **离线消息** — 用户上线自动推送未送达的私聊消息（最多 200 条/次）
- **在线状态** — 实时在线用户列表，服务端 WebSocket ping + 客户端心跳双保活，自动断线重连
- **开发模式** — 无 AuthSvc 时自动降级为 token-as-userID 模式，直接填写用户名即可使用
- **安全加固** — JWT 签名算法校验、认证重试限制、输入校验、XSS 防护、可配置 Origin 检查
- **暗色模式** — 自动跟随系统主题，支持手动切换
- **响应式布局** — 桌面/平板/手机自适应，移动端抽屉式侧边栏

## 前端界面

```
┌──────────┬──────────────────────────────────────┐
│ 用户名 ● │ ☰ # 房间名 / @用户名 (私聊)          │
├──────────┤                                      │
│ 房间     │   ┌──────────────────────────────┐   │
│  #lobby  │   │  用户A: 消息内容        时间  │   │
│  #tech   │   │  用户B: 另一条消息      时间  │   │
├──────────┤   └──────────────────────────────┘   │
│ 在线用户 │                                      │
│  ● 张三  │   ┌──────────────────────────────┐   │
│  ● 李四  │   │ 输入消息...             [发送]│   │
│          │   └──────────────────────────────┘   │
└──────────┴──────────────────────────────────────┘
```

- 左侧栏：在线用户列表 + 房间列表，点击切换会话
- 右侧：消息区域，支持群聊和私聊，气泡式消息展示
- 响应式设计，支持桌面和移动端（480px 以下为抽屉式侧边栏）
- 暗色模式支持

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.25 |
| WebSocket | gorilla/websocket |
| gRPC | google.golang.org/grpc |
| JWT | golang-jwt/jwt/v5 |
| 数据库 | MySQL 8.0 + MongoDB 7 |
| 缓存 | Redis 7 |
| 密码加密 | bcrypt |
| 配置 | spf13/viper |
| 前端 | 原生 HTML/CSS/JS（零依赖） |

## 快速开始

### 1. 配置环境变量

复制环境变量模板并填写：

```bash
cp .env.example .env
# 编辑 .env，设置 DB_DSN 和 JWT_SECRET（必填）
```

必填环境变量：

| 变量 | 说明 | 示例 |
|------|------|------|
| `DB_DSN` | MySQL 连接字符串 | `root:mypass@tcp(127.0.0.1:3306)/gochatx?parseTime=true` |
| `JWT_SECRET` | JWT 签名密钥（≥32 字符） | `your-random-secret-key-at-least-32-chars` |

可选环境变量：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis 地址 |
| `LISTEN_ADDR` | `:50051` | gRPC 监听地址 |

### 2. 启动依赖服务

```bash
# 如果使用 .env 文件，Docker Compose 会自动读取
docker compose up -d
```

启动 MySQL（3306）、MongoDB（27017）、Redis（6379）。

### 3. 启动认证服务

```bash
source .env  # 或 export DB_DSN=... JWT_SECRET=...
go run ./cmd/authsvc
# Auth service listening on :50051
# 首次启动自动创建 users 表
```

### 4. 启动网关

```bash
go run ./cmd/gateway
# Gateway listening on :8080
```

### 5. 打开前端

浏览器访问 `http://localhost:8080`

- **有 AuthSvc**：切换到「注册」创建账号 → 切换到「登录」获取 Token → 自动进入聊天
- **无 AuthSvc（开发模式）**：直接输入 UserID 点击登录即可

打开两个浏览器窗口，分别用不同用户名登录，即可测试群聊和私聊。

## REST API

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| POST | `/api/register` | 注册 | `{"username":"", "password":""}` |
| POST | `/api/login` | 登录 | `{"username":"", "password":""}` → 返回 `token` |
| GET | `/api/users/online` | 在线用户 | → `{"ok":true, "users":[...]}` |

## WebSocket 协议

连接地址：`ws://localhost:8080/ws`

### 客户端 → 服务端

| type | 说明 | 参数 |
|------|------|------|
| `auth` | 认证 | `token` — JWT token（开发模式下直接填 userID） |
| `join` | 加入房间 | `room_id` — 房间 ID |
| `leave` | 离开房间 | `room_id` |
| `send` | 发送群聊 | `room_id`, `content` |
| `private` | 发送私聊 | `to` — 目标用户ID, `content` |
| `history` | 查询历史 | `room_id`, `limit`（可选，默认 50） |
| `ping` | 心跳 | — |

### 服务端 → 客户端

| type | 说明 | 附带数据 |
|------|------|----------|
| `authed` | 认证成功 | `user` — 用户 ID |
| `joined` | 加入房间成功 | `room`, `history` — 最近 50 条消息 |
| `left` | 离开房间 | 房间名 |
| `message` | 群聊消息 | `from`, `room`, `ts`, `msg` |
| `private` | 私聊消息 | `from`, `to`, `ts`, `msg` |
| `history` | 历史消息结果 | 消息数组 |
| `ack` | 私聊发送确认 | `to` |
| `pong` | 心跳响应 | 时间戳 |
| `error` | 错误信息 | 错误描述 |

## 配置

AuthSvc 通过环境变量配置（敏感信息无默认值，必须显式设置）：

| 环境变量 | 必填 | 默认值 | 说明 |
|----------|------|--------|------|
| `DB_DSN` | ✅ | — | MySQL DSN |
| `JWT_SECRET` | ✅ | — | JWT 签名密钥（建议 ≥32 字符） |
| `REDIS_ADDR` | | `127.0.0.1:6379` | Redis 地址 |
| `LISTEN_ADDR` | | `:50051` | gRPC 监听地址 |

Gateway 通过 `config.yaml` 或 viper 默认值配置，见文件内注释。

## 安全特性

- **JWT 签名算法校验** — 防止 `alg:none` 攻击
- **JWT Claims 完整性** — 包含 Issuer、Subject、IssuedAt、ExpiresAt
- **认证重试限制** — WebSocket 连接最多 3 次认证尝试
- **输入校验** — 用户名 2-32 字符，密码 6-72 字符
- **bcrypt 密码哈希** — 默认 cost 值
- **XSS 防护** — 前端所有用户数据通过 `escapeHtml` 或 `textContent` 渲染
- **WebSocket 读超时** — 60 秒无消息自动断开，服务端 30 秒 ping 帧保活
- **Origin 检查** — WebSocket `CheckOrigin` 可配置
- **HTTP 超时** — Read/Write/Idle/ReadHeader 均有超时配置
- **环境变量管理** — 敏感信息不硬编码，提供 `.env.example` 模板

## 项目结构

```
GoChatX/
├── api/                       # gRPC proto 定义及生成代码
│   ├── auth.proto
│   ├── auth.pb.go
│   └── auth_grpc.pb.go
├── cmd/
│   ├── authsvc/main.go        # 认证服务入口
│   └── gateway/main.go        # 网关入口（REST + WebSocket + 静态文件）
├── internal/
│   ├── auth/service.go        # 认证逻辑（注册/登录/JWT/bcrypt）
│   ├── gateway/
│   │   ├── api.go             # REST API 处理（注册/登录/在线用户）
│   │   ├── clients.go         # WebSocket 客户端 + 在线用户管理
│   │   ├── room.go            # 房间管理（创建/加入/广播/离开）
│   │   └── ws_handle.go       # WebSocket 消息处理主逻辑
│   └── storage/mango.go       # MongoDB 存储层（索引/错误处理）
├── web/
│   ├── index.html             # 前端 SPA 页面
│   ├── style.css              # 样式（暗色模式 + 响应式）
│   └── app.js                 # 前端逻辑（指数退避重连/XSS 防护）
├── config.yaml                # 配置文件
├── .env.example               # 环境变量模板
├── docker-compose.yml         # 依赖服务编排
├── go.mod
└── go.sum
```

## 降级策略

| 组件不可用 | 影响 | 降级行为 |
|-----------|------|---------|
| authsvc | 无法注册/登录 | 直接用用户名作为 userID（开发模式） |
| MongoDB | 无消息持久化 | 房间消息仅广播不存储，历史查询返回空 |
| Redis | 无在线状态 Redis 标记 | 内存在线表仍可用，仅 Redis 中无记录 |
| MySQL | authsvc 启动失败 | authsvc 无法运行 |
