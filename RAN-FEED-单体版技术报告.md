# RAN·FEED 单体架构技术报告（保留全部业务能力，移除微服务拆分）

> **目标版本**：Go 1.25+ | go-zero v1.9.4  
> **文档用途**：作为项目目录内的长期技术说明文档，供 Codex / 开发者随时阅读、改造与实现  
> **改造原则**：**仅移除微服务架构与 RPC 拆分，其他业务能力、缓存设计、异步机制、存储模型、可观测性与运维能力全部保留**  
> **来源说明**：本报告基于原始《RAN·FEED 深度技术解析报告》重构而成，重构重点是把“分布式服务协作”改写为“单体应用内模块协作”。

---

## 目录

- [一、改造目标与总原则](#一改造目标与总原则)
- [二、单体版整体技术栈](#二单体版整体技术栈)
- [三、单体架构设计](#三单体架构设计)
  - [3.1 总体分层](#31-总体分层)
  - [3.2 模块划分](#32-模块划分)
  - [3.3 模块依赖关系](#33-模块依赖关系)
  - [3.4 数据流转全景](#34-数据流转全景)
- [四、目录结构改造建议](#四目录结构改造建议)
- [五、核心功能实现逻辑（全部保留）](#五核心功能实现逻辑全部保留)
  - [5.1 Feed 流引擎](#51-feed-流引擎)
  - [5.2 内容发布管线](#52-内容发布管线)
  - [5.3 互动系统](#53-互动系统)
  - [5.4 用户认证与会话管理](#54-用户认证与会话管理)
  - [5.5 计数体系](#55-计数体系)
- [六、关键实现从“RPC 调用”改为“模块调用”](#六关键实现从rpc-调用改为模块调用)
  - [6.1 调用方式改造原则](#61-调用方式改造原则)
  - [6.2 推荐流改造](#62-推荐流改造)
  - [6.3 关注流改造](#63-关注流改造)
  - [6.4 点赞链路改造](#64-点赞链路改造)
  - [6.5 用户信息与批量聚合改造](#65-用户信息与批量聚合改造)
- [七、高并发与高吞吐设计（保留）](#七高并发与高吞吐设计保留)
- [八、Lua、异步、消息队列与定时任务（保留）](#八lua异步消息队列与定时任务保留)
- [九、并发安全与稳定性设计（保留）](#九并发安全与稳定性设计保留)
- [十、部署架构（单体版）](#十部署架构单体版)
- [十一、Codex 可直接执行的改造指引](#十一codex-可直接执行的改造指引)
- [十二、结论](#十二结论)

---

## 一、改造目标与总原则

本次改造不是重做项目，而是**架构形态调整**。

### 1.1 必须保留的内容

以下能力**全部保留，不允许因改单体而删除**：

1. Feed 核心能力：推荐流、关注流、用户发布列表、用户收藏列表。  
2. 内容能力：文章发布、视频发布、删除内容、内容详情、上传凭证。  
3. 互动能力：点赞、取消点赞、收藏、取消收藏、评论、删除评论、回复、关注、取消关注。  
4. 用户能力：登录、注册、注销、获取自己信息、获取他人信息、批量查询用户、更新资料、上传头像。  
5. 计数能力：点赞数、收藏数、评论数、获赞数、被收藏数等。  
6. Redis 设计：热榜 ZSET、关注收件箱 ZSET、用户点赞 HASH、Session 双向映射、评论缓存、Lua 原子脚本。  
7. 异步设计：Kafka 事件、GoSafe 异步任务、定时任务、Canal 对 Binlog 的订阅。  
8. 可观测性：OpenTelemetry、Jaeger、Prometheus、Grafana、ELK。  
9. 工程能力：统一错误体系、统一中间件、统一日志、统一配置、统一 ID 生成。  

### 1.2 唯一被移除的内容

本次**只移除以下“微服务形态”内容**：

- front-api 作为独立网关进程
- content-rpc / interaction-rpc / user-rpc / count-rpc 多进程部署
- gRPC 服务间调用
- Protobuf 作为进程间协议的必选项
- Etcd 服务注册发现
- zRPC 客户端配置、服务间地址发现、NonBlock 连接语义
- “跨服务边界”带来的 DTO 拆分、client stub、server stub、RPC error mapping

### 1.3 单体改造后的核心思想

改单体后，项目从“多个进程协作”变为“**一个进程内多个领域模块协作**”。

也就是说：

- **服务边界保留，但变成模块边界**。
- **RPC 接口保留语义，但改成内部接口 / application service 方法**。
- **数据库、Redis、Kafka、Canal、OSS 等基础设施继续存在**。
- **所有业务流转、缓存策略、异步设计、冷热分层、分页机制全部继续沿用**。

一句话总结：

> **拆掉的是“分布式进程边界”，保留的是“领域边界与业务实现”。**

---

## 二、单体版整体技术栈

### 2.1 单体版核心技术全景图

| 层次 | 技术 | 定位 |
|------|------|------|
| 编程语言 | Go 1.25+ | 高性能主语言，原生协程支持 |
| Web 框架 | go-zero REST | HTTP API、路由、中间件、配置管理 |
| 业务组织方式 | 单体分层 + DDD 模块化 | 按领域拆分 package，而不是拆分进程 |
| 数据库 | MySQL 8.x | 核心业务持久化 |
| ORM | GORM + GORM Gen | 类型安全查询、复杂 SQL 构造 |
| 缓存 | Redis | 热榜、收件箱、Session、状态缓存、分布式锁 |
| 消息队列 | Kafka | 异步事件投递与削峰 |
| Binlog 订阅 | Canal | MySQL 增量数据订阅 |
| 对象存储 | 阿里云 OSS | 图片 / 视频 / 文件存储 |
| 定时任务 | XXL-Job 或内嵌 Cron | 热榜重建、缓存回填、补偿任务 |
| 反向代理 | Nginx | 反向代理、SSL、限流 |
| 容器编排 | Docker Compose | 单体应用 + 基础设施一键部署 |
| 可观测性 | OTel + Jaeger + Prometheus + Grafana + ELK | 追踪、指标、日志 |
| 鉴权 | JWT + Redis Session | 登录态校验与滑动续期 |
| ID 生成 | Sonyflake | 全局唯一 ID |
| 参数校验 | validator | 请求体结构化校验 |

### 2.2 单体版对 go-zero 的定位

改单体后，go-zero 不再承担“微服务治理框架”的角色，而是承担：

- REST API 服务框架
- 配置装载器
- 中间件承载层
- 日志框架承载层
- 并发工具承载层（`mr`、`threading`）
- Redis 工具与工程基础设施整合层

因此，go-zero 仍然非常适合保留，只是使用重心从 **go-rest + zRPC** 变为 **go-rest + 内部模块化调用**。

### 2.3 保留 Kafka + Canal 的原因

即使改单体，也**不建议删除 Kafka 和 Canal**。

原因很简单：

1. 它们解决的是**异步解耦与削峰**问题，而不是微服务专属问题。  
2. 点赞、关注、计数更新、热榜增量这些链路天然适合事件驱动。  
3. 单体应用仍然可能面对高并发写入，Kafka 仍然有价值。  
4. Canal 仍然适合从 MySQL Binlog 中订阅增量变化，用于推模式的 Feed 更新或统计修正。  

结论：

> **Kafka / Canal 是业务吞吐与解耦方案，不是微服务专属设施。改单体后继续保留。**

---

## 三、单体架构设计

## 3.1 总体分层

改单体后，推荐采用如下结构：

```text
┌─────────────────────────────────────────────────────────────┐
│                         客户端层                             │
│                  Web / Mobile / 第三方调用                    │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTPS
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                       Nginx 反向代理                         │
│                 SSL / 限流 / 静态资源 / 负载转发              │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTP
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    RAN·FEED 单体应用                         │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ API 层：Handler / Route / DTO / Middleware            │  │
│  └──────────────────────┬────────────────────────────────┘  │
│                         ▼                                   │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ Application 层：UseCase / Logic / Command / Query     │  │
│  └──────────────────────┬────────────────────────────────┘  │
│                         ▼                                   │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ Domain 模块层                                         │  │
│  │ Content / Feed / Interaction / User / Count           │  │
│  └──────────────────────┬────────────────────────────────┘  │
│                         ▼                                   │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ Infrastructure 层                                      │  │
│  │ MySQL / Redis / Kafka / Canal / OSS / Job / Trace     │  │
│  └───────────────────────────────────────────────────────┘  │
└──────────────────────┬───────────────┬───────────────┬──────┘
                       ▼               ▼               ▼
                    MySQL            Redis           Kafka
```

### 3.2 模块划分

虽然不再拆成多个服务，但仍然建议保留原有领域边界：

| 模块 | 原微服务来源 | 单体中的角色 |
|------|-------------|-------------|
| `content` | content-rpc | 内容生命周期管理 |
| `feed` | content-rpc 中 FeedService | 推荐流、关注流、用户发布、用户收藏 |
| `interaction` | interaction-rpc | 点赞、收藏、评论、关注 |
| `user` | user-rpc | 用户认证、会话、资料 |
| `count` | count-rpc | 各类聚合计数 |
| `api` | front-api | HTTP 接口、参数解析、统一返回 |
| `pkg` | 公共库 | 错误、ORM、JWT、雪花 ID、工具、拦截器 |

### 3.3 模块依赖关系

改单体后，推荐依赖关系如下：

```text
api
 └── application
      ├── feed
      │    ├── content
      │    ├── user
      │    ├── interaction
      │    └── count
      ├── content
      │    ├── user
      │    └── count
      ├── interaction
      │    ├── content
      │    ├── user
      │    └── count
      ├── user
      └── count
```

这里的关键变化是：

- 以前：`front -> RPC Client -> 下游服务`
- 现在：`api handler -> application service -> domain service / repository`

### 3.4 数据流转全景

以“用户查看推荐 Feed”为例，改单体后的完整链路如下：

```text
[客户端] GET /v1/feed/recommend
    │
    ▼
[Nginx]
    │
    ▼
[单体应用 API 层]
    │
    ├── OptionalLoginMiddleware
    │    ├── 有 token：verifyAndRenewSession() → Redis Lua
    │    └── 无 token：匿名放行
    │
    ▼
[FeedApplicationService.RecommendFeed]
    │
    ├── resolveSnapshotKey()
    ├── queryHotIDsByCursor() → Redis Lua
    ├── contentRepo.BatchGetRecommendByIDs() → MySQL
    ├── buildBriefMaps() → article/video repository
    ├── buildUserAndLikeMaps() → 直接调用 user / interaction / count 模块
    └── buildItems() → 返回前端视图模型
```

这里最重要的变化是：

- **没有网络 hop**
- **没有 gRPC 序列化/反序列化**
- **没有服务发现**
- **没有跨服务错误码转换**

但：

- Redis 还是 Redis
- MySQL 还是 MySQL
- Kafka 还是 Kafka
- Feed 算法、缓存策略、分页逻辑、Lua 脚本都不变

---

## 四、目录结构改造建议

建议将原多服务目录改造成如下单体结构：

```text
ran-feed/
├── cmd/
│   └── ranfeed/
│       └── main.go                     # 单体应用入口
│
├── configs/
│   ├── app.yaml                        # 主配置
│   ├── redis.yaml
│   ├── mysql.yaml
│   ├── kafka.yaml
│   └── observability.yaml
│
├── internal/
│   ├── api/
│   │   ├── handler/
│   │   ├── routes/
│   │   ├── middleware/
│   │   ├── types/
│   │   └── assembler/
│   │
│   ├── application/
│   │   ├── content/
│   │   ├── feed/
│   │   ├── interaction/
│   │   ├── user/
│   │   └── count/
│   │
│   ├── domain/
│   │   ├── content/
│   │   │   ├── entity/
│   │   │   ├── repository/
│   │   │   └── service/
│   │   ├── feed/
│   │   ├── interaction/
│   │   ├── user/
│   │   └── count/
│   │
│   ├── infrastructure/
│   │   ├── db/
│   │   ├── redis/
│   │   ├── kafka/
│   │   ├── oss/
│   │   ├── job/
│   │   ├── trace/
│   │   └── mq/
│   │
│   └── svc/
│       └── service_context.go          # 单体统一依赖注入中心
│
├── pkg/
│   ├── errorx/
│   ├── orm/
│   ├── jwt/
│   ├── snowflake/
│   ├── result/
│   ├── validate/
│   ├── hotrank/
│   └── utils/
│
├── deploy/
│   ├── docker-compose.yml              # 单体版 compose
│   ├── nginx/
│   ├── kafka/
│   ├── canal/
│   ├── prometheus/
│   ├── grafana/
│   ├── elasticsearch/
│   ├── logstash/
│   ├── filebeat/
│   └── mysql/
│
├── script/
├── docs/
└── README.md
```

### 4.1 原目录到新目录的映射建议

| 原目录 | 新位置 | 说明 |
|-------|-------|------|
| `app/front/internal/handler` | `internal/api/handler` | 保留 HTTP 接口层 |
| `app/front/internal/middleware` | `internal/api/middleware` | 原样保留 |
| `app/front/internal/types` | `internal/api/types` | 请求/响应结构体 |
| `app/rpc/content/internal/logic/contentservice` | `internal/application/content` | 改成内容应用服务 |
| `app/rpc/content/internal/logic/feedservice` | `internal/application/feed` | 改成 Feed 应用服务 |
| `app/rpc/interaction/internal/logic` | `internal/application/interaction` | 改成互动应用服务 |
| `app/rpc/user/internal/...` | `internal/application/user` | 改成用户应用服务 |
| `app/rpc/count/internal/...` | `internal/application/count` | 改成计数应用服务 |
| 各 RPC `repositories/entity/do/query` | `internal/domain/*` | 按领域吸收 |
| `proto/*.proto` | `docs/contracts/` 或删除 | 不再作为进程间协议必需品 |

---

## 五、核心功能实现逻辑（全部保留）

## 5.1 Feed 流引擎

Feed 仍然是系统最核心的子系统，继续采用：

- **推荐流：拉模式为主 + 热榜缓存 + 快照分页**
- **关注流：推模式为主 + 收件箱 + 冷启动回填**

### 5.1.1 推荐流（Recommend Feed）

核心架构保持不变：

```text
定时任务 / 事件增量
    │
    ▼
热度计算
    │
    ▼
Redis ZSET: feed:hot:global
    │
    ├── 周期性生成快照 feed:hot:global:snap:{snapshotId}
    └── latest snapshot pointer
    │
    ▼
用户请求推荐流
    │
    ▼
读取快照 / 全局热榜
    │
    ▼
批量查 MySQL 内容数据
    │
    ▼
补齐作者、点赞、计数、摘要
    │
    ▼
返回 Feed 列表
```

保留的关键机制：

1. **三级降级读取策略**：优先使用指定快照，其次最新快照，最后全局热榜。  
2. **游标分页**：继续使用 score + content_id 去重。  
3. **过扫描策略**：继续 `pageSize + 32`。  
4. **快照隔离**：分页过程中视图稳定，不会“翻页跳动”。  
5. **热榜增量合并**：Hash 增量桶 + Lua 原子 merge。  

### 5.1.2 关注流（Follow Feed）

关注流架构也保持不变：

```text
作者发布内容
   │
   ├── 写数据库
   ├── 发事件 / Canal 订阅
   └── 更新粉丝收件箱 ZSET

用户读取关注流
   │
   ├── 先查 Redis inbox
   ├── 若不存在则冷启动回填
   └── 批量组装内容、作者、状态、计数
```

保留的关键能力：

- 粉丝收件箱 `feed:follow:inbox:{userId}` 使用 ZSET 存时间序
- 冷启动时通过分布式锁防止并发重建
- 收件箱容量控制，例如 `keepN=5000`
- 内容详情、作者信息、点赞状态、计数统一批量装配

### 5.1.3 Feed 装配模式保持不变

改单体后，Feed 装配仍然建议保持五步：

1. 批量查询内容基础信息  
2. 按文章 / 视频类型查询摘要  
3. 并行查询作者信息与互动状态  
4. 组装统一视图对象  
5. 构造分页响应  

变的只是调用方式，不是逻辑本身。

## 5.2 内容发布管线

内容发布继续保持“客户端直传 OSS + 服务端提交发布”的模式：

```text
① 请求上传凭证
② 客户端直传 OSS
③ 提交文章 / 视频发布请求
④ 服务端生成 content_id
⑤ 写入 content 主表 + article/video 子表
⑥ 发送后续事件（用于热榜 / 推送 / 统计）
```

必须保留的能力：

- 文章与视频两种内容类型
- `ContentType / ContentStatus / Visibility` 枚举语义
- 上传凭证签发
- 内容删除
- 内容详情查询
- 用户发布数量查询
- 后续异步链路（推送到粉丝收件箱、更新热榜、刷新统计）

## 5.3 互动系统

互动模块继续保留四块：

1. 点赞  
2. 收藏  
3. 评论  
4. 关注  

### 5.3.1 点赞系统

点赞系统的 Redis HASH + Lua 原子脚本设计必须完整保留。

保留原因：

- `HSETNX` 保证幂等
- `_mincid` 元字段支持容量淘汰
- `_expire_at / _ver` 保持缓存状态管理能力
- 点赞后异步发 Kafka 事件更新计数与热榜增量

### 5.3.2 收藏系统

收藏仍然保留：

- 收藏 / 取消收藏
- 收藏列表查询
- 收藏数更新
- 内容详情中的 `is_favorited` 状态

### 5.3.3 评论系统

评论系统继续保留嵌套回复模型：

- `root_id`
- `parent_id`
- `reply_to_user_id`
- 评论列表 / 回复列表分页
- 评论缓存与回填

### 5.3.4 关注系统

关注系统继续保留：

- 关注 / 取消关注
- 关注列表查询
- 粉丝与关注统计
- 关注关系变更对 Feed 收件箱的影响

## 5.4 用户认证与会话管理

用户模块不再是独立 `user-rpc`，但功能完全保留：

- 登录
- 注册
- 注销
- GetMe
- GetUser
- BatchGetUser
- UpdateProfile
- UploadAvatar

继续保留的关键设计：

### 5.4.1 双向 Session 映射

```text
user:session:{token}         -> userId
user:session:user:{userId}   -> token
```

### 5.4.2 Lua 原子校验 + 续期

`verify_and_renew_session.lua` 继续使用，不做删除。

理由：

- 这是会话一致性与安全性的关键脚本
- 与是否微服务无关
- 续期阈值、双向校验、TTL 更新逻辑都仍然需要

## 5.5 计数体系

计数模块虽然不再独立部署，但建议仍然作为**独立领域模块**保留。

继续负责：

- 内容点赞数
- 内容收藏数
- 内容评论数
- 用户获赞数
- 用户被收藏数
- 其他聚合计数

原因：

1. 计数是高频写、高频读的典型热点领域。  
2. 即使改单体，也需要单独演进缓存策略和更新方式。  
3. 计数逻辑与互动操作本身最好解耦，便于后续继续事件化。  

---

## 六、关键实现从“RPC 调用”改为“模块调用”

## 6.1 调用方式改造原则

原项目的主要调用方式是：

```go
front logic -> RPC client -> user/content/interaction/count service
```

改单体后统一改成：

```go
handler -> application service -> domain service / repository
```

### 6.1.1 改造规则

1. 删除所有 `RpcClientConf`。  
2. 删除 `zrpc.MustNewClient(...)` 与自动生成 client。  
3. 将 `svcCtx.UserRpc.BatchGetUser(...)` 改为 `svcCtx.UserApp.BatchGetUser(...)`。  
4. 将 `svcCtx.ContentRpc.RecommendFeed(...)` 改为 `svcCtx.FeedApp.RecommendFeed(...)`。  
5. 将原 Proto Request/Response 改为内部 DTO 或直接复用已有结构体。  
6. 将 gRPC interceptor 中的错误转换逻辑收敛到统一业务错误处理层。  
7. 将“跨服务超时控制”改为“模块方法上下文控制”。  

### 6.1.2 ServiceContext 的新职责

改单体后，`ServiceContext` 不再持有 RPC Client，而是持有模块服务实例：

```go
type ServiceContext struct {
    Config Config

    DB      *orm.DB
    Redis   *redis.Redis
    Kafka   *kafka.Manager
    OSS     *oss.Client

    ContentApp     *content.ApplicationService
    FeedApp        *feed.ApplicationService
    InteractionApp *interaction.ApplicationService
    UserApp        *user.ApplicationService
    CountApp       *count.ApplicationService
}
```

## 6.2 推荐流改造

原来推荐流通常是：

```text
RecommendHandler
  -> front logic
  -> ContentRpc FeedService.RecommendFeed
  -> 下游 user-rpc / interaction-rpc 批量查询
```

改单体后建议变成：

```text
RecommendHandler
  -> FeedApplicationService.RecommendFeed
     -> ContentRepository
     -> UserApplicationService.BatchGetUser
     -> InteractionApplicationService.BatchQueryLikeInfo
     -> CountApplicationService.BatchGetContentCounters
```

### 6.2.1 改造收益

- 去掉 gRPC 序列化成本
- 去掉网络传输成本
- 去掉 Etcd 发现成本
- 去掉 client / server stub 维护成本
- 保留批量查询与并行聚合能力

### 6.2.2 推荐流伪代码

```go
func (s *FeedApplicationService) RecommendFeed(ctx context.Context, req *RecommendFeedReq) (*RecommendFeedRes, error) {
    preferredKey, snapshotID := s.resolveSnapshotKey(req.SnapshotId)

    ids, nextCursor, hasMore, err := s.queryHotIDsByCursor(ctx, preferredKey, snapshotID, req.Cursor, req.PageSize)
    if err != nil {
        return nil, err
    }
    if len(ids) == 0 {
        return &RecommendFeedRes{Items: []ContentItem{}}, nil
    }

    contents, err := s.contentRepo.BatchGetRecommendByIDs(ctx, ids)
    if err != nil {
        return nil, err
    }

    articleMap, videoMap, err := s.buildBriefMaps(ctx, contents)
    if err != nil {
        return nil, err
    }

    userMap, likedMap, countMap, err := s.buildUserAndLikeMaps(ctx, req.UserId, contents)
    if err != nil {
        return nil, err
    }

    items := s.buildItems(contents, articleMap, videoMap, userMap, likedMap, countMap)
    return &RecommendFeedRes{Items: items, NextCursor: nextCursor, HasMore: hasMore, SnapshotId: snapshotID}, nil
}
```

## 6.3 关注流改造

关注流改造后同样只改调用方式，不改行为逻辑。

原行为必须保留：

- 先查 inbox ZSET
- 缓存不存在时触发冷启动回填
- 回填过程用分布式锁保护
- 回填后通过重试或下次请求读取

改单体后，回填时不再调用 `FollowRpc.ListFollowees`，而是直接调用用户/互动模块中的关注关系查询能力。

```go
followees, err := s.interactionApp.ListFollowees(ctx, userID, cursor, limit)
```

而不是：

```go
resp, err := s.svcCtx.FollowRpc.ListFollowees(...)
```

## 6.4 点赞链路改造

原链路：

```text
front -> interaction-rpc LikeService -> Redis Lua -> Kafka -> count-rpc/content-rpc
```

改单体后：

```text
HTTP Handler
   -> InteractionApplicationService.Like
      -> Redis Lua
      -> GoSafe publishLikeEvent
         -> Kafka
            -> 单体内消费者 / 任务处理器更新 count、hotrank
```

也就是说：

- 点赞主路径保持极短
- 事件仍然异步化
- 消费者不再是远程服务，而是单体内部消费者模块

## 6.5 用户信息与批量聚合改造

原先 Feed 为了避免 N+1，会调用批量用户查询与批量点赞状态查询。改单体后这一设计继续保留，只是接口变为本地方法：

```go
userMap, err := s.userApp.BatchGetUserMap(ctx, authorIDs)
likeMap, err := s.interactionApp.BatchQueryLikeInfoMap(ctx, userID, likeInfos)
countMap, err := s.countApp.BatchGetContentCountMap(ctx, contentIDs)
```

这个批量设计**不能删除**，因为它解决的是性能问题，不是服务边界问题。

---

## 七、高并发与高吞吐设计（保留）

改单体后，以下高并发设计全部继续有效：

### 7.1 goroutine 并发编排

保留：

- `threading.GoSafe()` 用于异步事件投递
- `mr.Finish()` 用于 Feed 聚合并行查询
- 少量原生 `go func()` 用于辅助任务

### 7.2 多级缓存体系

继续保留：

| 数据 | Redis 结构 | 作用 |
|------|-----------|------|
| 热榜 | ZSET | 推荐排序 |
| 关注收件箱 | ZSET | 关注流 |
| 用户点赞 | HASH | 点赞状态 |
| Session | String | 登录态 |
| 评论缓存 | Hash/String | 评论展示 |
| 热榜增量 | Hash | 增量分值 |

### 7.3 游标分页

继续全面使用 Cursor-based Pagination：

- 推荐流
- 关注流
- 评论列表
- 回复列表
- 用户发布列表
- 用户收藏列表

### 7.4 批量聚合

继续保留批量查询与批量组装策略，避免 N+1：

- BatchGetUser
- BatchQueryLikeInfo
- BatchGetCount
- BatchGetCommentPreview

### 7.5 单体后的性能额外收益

改单体后，理论上还能获得以下额外收益：

1. gRPC 编解码开销消失  
2. 跨服务网络延迟消失  
3. 错误码映射层级减少  
4. 配置复杂度降低  
5. 部署与联调更简单  

---

## 八、Lua、异步、消息队列与定时任务（保留）

## 8.1 Lua 脚本全部保留

原项目中的 24 个 Lua 脚本应全部保留，至少包括：

- 点赞写入脚本
- 取消点赞脚本
- Session 校验续期脚本
- 热榜查询脚本
- 热榜增量合并脚本
- 热榜快照重建脚本
- 关注收件箱查询脚本
- 关注收件箱更新脚本
- 评论查询脚本
- 评论缓存回填脚本

原因：

> Lua 保证的是 Redis 复合操作原子性，这与单体 / 微服务无关。

## 8.2 Kafka 继续保留

单体中 Kafka 可有两种形态：

### 方案 A：继续使用外部 Kafka

适合：

- 仍然追求削峰与可扩展异步处理
- 后续可能再拆服务
- 希望保留更强的事件可追溯性

### 方案 B：局部替换为进程内事件总线

仅适用于低并发、开发环境或极简部署场景。

**但当前报告建议保留 Kafka**，因为你明确要求“其他所有功能实现全部保留”。

## 8.3 Canal 继续保留

Canal 的作用是监听 MySQL Binlog 并驱动后续更新，改单体后依然可以：

- 更新粉丝收件箱
- 修正计数缓存
- 触发热榜增量累加
- 做异步补偿

## 8.4 定时任务继续保留

热榜重建、快照刷新、缓存补偿等任务继续保留。

可以选择两种实现：

1. 继续保留 XXL-Job。  
2. 改成单体内部 cron / scheduler。  

若追求“原功能不变”，建议优先保留 XXL-Job。

---

## 九、并发安全与稳定性设计（保留）

## 9.1 Redis Lua 原子性

所有“读-判-写”复合操作继续放在 Lua 中：

- 点赞写入 / 取消
- Session 校验 / 续期
- 热榜查询 / 合并 / 快照重建
- 收件箱查询 / 更新
- 评论缓存查询与补偿

## 9.2 分布式锁

虽然现在是单体应用，但只要部署多个实例，依然需要 Redis 分布式锁。

因此以下锁设计继续保留：

- 关注流收件箱冷启动重建锁
- 其他可能的重建 / 回填锁

结论：

> **改单体不等于单实例。只要可能水平扩展，分布式锁就仍然必要。**

## 9.3 统一错误体系

保留 `pkg/errorx`：

- `BizError`
- `Wrap`
- 默认错误码
- 隐藏系统错误细节
- 统一日志输出

### 9.3.1 需要删除的只有 gRPC 专属部分

以下内容可以删除或弱化：

- `grpcx.FromError`
- `grpcx.ToError`
- gRPC interceptor 中的协议映射逻辑

但以下内容仍需保留：

- panic recover
- stack capture
- 业务错误与系统错误分层

## 9.4 会话安全

会话安全设计不变：

- 双向 session 校验
- TTL 滑动续期
- token 失效后自动退出
- 单用户覆盖旧登录态（如需单端登录语义）

## 9.5 连接池与资源管理

改单体后仍需配置：

- MySQL 连接池
- Redis 连接池
- Kafka producer / consumer 生命周期
- OSS client 生命周期

ServiceContext 统一管理这些基础设施资源即可。

## 9.6 可观测性继续保留

OpenTelemetry、Prometheus、ELK 继续保留，只是 trace 结构会从“跨服务 span”变成“单体内分层 span”。

这反而有一个好处：

- 链路更短、更清晰
- 调试时少了服务边界噪音

---

## 十、部署架构（单体版）

## 10.1 单体版 Docker Compose 建议

改单体后，应用服务从多进程缩减为一个主应用：

| 分类 | 服务 | 端口 | 说明 |
|------|------|------|------|
| 应用服务 | ranfeed-app | 5000 | 单体应用 |
| 前端/代理 | nginx | 80 | 反向代理 |
| 基础设施 | mysql | 3306 | 持久化存储 |
| 基础设施 | redis | 6379 | 缓存 |
| 基础设施 | kafka | 9092 | 消息队列 |
| 数据处理 | canal | 11111 | Binlog 订阅 |
| 定时任务 | xxl-job-admin | 8080 | 任务调度中心 |
| 观测 | otel-collector | 4317/4318 | tracing |
| 观测 | jaeger | 16686 | trace UI |
| 观测 | prometheus | 9090 | metrics |
| 观测 | grafana | 3000 | metrics UI |
| 观测 | elasticsearch | 9200 | 日志存储 |
| 观测 | logstash | 5044 | 日志管道 |
| 观测 | kibana | 5601 | 日志检索 |
| 观测 | filebeat | - | 日志采集 |

### 10.2 可以删除的部署对象

以下内容可从部署层删除：

- content-rpc 容器
- interaction-rpc 容器
- user-rpc 容器
- count-rpc 容器
- front-api 容器
- Etcd 容器

### 10.3 单体版启动顺序

```text
mysql / redis / kafka / canal / otel 等基础设施启动
    ↓
ranfeed-app 启动
    ↓
nginx 启动或 reload
```

### 10.4 单体版配置结构建议

```yaml
Name: ranfeed-app
Host: 0.0.0.0
Port: 5000

MySQL:
  DSN: xxx
  MaxOpenConns: 100
  MaxIdleConns: 10
  MaxLifetime: 3600

Redis:
  Host: xxx
  Pass: xxx
  Type: node

Kafka:
  Brokers:
    - xxx:9092
  Topics:
    LikeEvents: like-events
    FavoriteEvents: favorite-events

OSS:
  Endpoint: xxx
  Bucket: xxx

SessionTTL: 86400

Telemetry:
  Name: ranfeed-app
  Endpoint: otel-collector:4317

Prometheus:
  Host: 0.0.0.0
  Port: 9290
```

---

## 十一、Codex 可直接执行的改造指引

这一节专门给 Codex / 自动化改造使用。

## 11.1 第一阶段：结构收拢

1. 新建单体入口 `cmd/ranfeed/main.go`。  
2. 新建统一 `internal/svc/service_context.go`。  
3. 将 front 的 handler、middleware、types 迁移到 `internal/api`。  
4. 将 content/feed/interaction/user/count 的 logic 迁移到 `internal/application/*`。  
5. 将 repository/entity/query/do 迁移到 `internal/domain/*`。  

## 11.2 第二阶段：删除 RPC 依赖

1. 删除所有 `zrpc` 相关 client 初始化。  
2. 删除各 `proto` 自动生成 client/server 代码依赖。  
3. 删除 Etcd 配置与服务发现配置。  
4. 删除 gRPC server 启动代码。  
5. 将原 `xxxRpc.SomeMethod(...)` 替换为模块方法调用。  

### 11.2.1 典型替换规则

| 原调用 | 新调用 |
|------|------|
| `svcCtx.ContentRpc.RecommendFeed(ctx, req)` | `svcCtx.FeedApp.RecommendFeed(ctx, req)` |
| `svcCtx.UserRpc.BatchGetUser(ctx, req)` | `svcCtx.UserApp.BatchGetUser(ctx, req)` |
| `svcCtx.LikesRpc.BatchQueryLikeInfo(ctx, req)` | `svcCtx.InteractionApp.BatchQueryLikeInfo(ctx, req)` |
| `svcCtx.CountRpc.GetCounts(ctx, req)` | `svcCtx.CountApp.GetCounts(ctx, req)` |

## 11.3 第三阶段：DTO 统一

建议把原来分散的三套对象收敛：

- HTTP Request / Response：保留在 `internal/api/types`
- 应用层 DTO：放在 `internal/application/*/dto.go`
- 数据库实体：放在 `internal/domain/*/entity`

不再需要：

- Proto message 仅为传输存在的镜像结构体
- gRPC 错误包装对象

## 11.4 第四阶段：消费端内聚

Kafka 的消费者不再分散在多个服务中，而是在单体应用启动时统一注册：

```go
func RegisterConsumers(ctx *ServiceContext) {
    ctx.Kafka.Register("like-events", interaction.NewLikeEventHandler(ctx))
    ctx.Kafka.Register("favorite-events", interaction.NewFavoriteEventHandler(ctx))
    ctx.Kafka.Register("follow-events", feed.NewFollowInboxHandler(ctx))
}
```

## 11.5 第五阶段：测试重建

改单体后必须补齐以下测试：

1. Feed 推荐流集成测试  
2. 关注流冷启动测试  
3. 点赞 Lua 幂等性测试  
4. Session 续期脚本测试  
5. Kafka 事件消费测试  
6. 评论树查询测试  
7. 单体版 ServiceContext 启动测试  

## 11.6 第六阶段：文档与配置清理

Codex 应自动执行以下清理：

- 删除 README 中的多服务启动说明
- 删除 RPC 服务端口说明
- 删除 Etcd 依赖描述
- 删除“服务注册发现”章节
- 将“领域服务拆分”改写为“领域模块划分”
- 将“服务通信”改写为“进程内模块调用”

---

## 十二、结论

这次改单体的本质，不是做减法式阉割，而是做**部署形态收敛**。

### 12.1 最终保留的核心价值

保留的不是表面模块名，而是整套成熟实现：

- Feed 快照与分页机制
- Redis 多结构缓存体系
- Lua 原子脚本体系
- 点赞 / 收藏 / 评论 / 关注完整链路
- Kafka 异步事件架构
- Canal 增量订阅能力
- 定时任务与热榜重建机制
- 会话安全与滑动续期
- 可观测性与统一错误体系

### 12.2 单体版的优势

改单体后，项目会获得这些现实收益：

1. 部署更简单  
2. 本地开发更简单  
3. 调试路径更短  
4. 配置更少  
5. Codex 更容易理解与改造  
6. 在中小团队与单仓开发场景下更高效  

### 12.3 最终结论

> **RAN·FEED 应改造成“单体应用 + 领域模块化 + Redis/Kafka/Canal/OSS 等基础设施保留”的架构。**
>
> **只删除微服务拆分方式，不删除任何业务能力、缓存设计、异步机制和工程能力。**
>
> **所有原本跨服务的调用，统一改为进程内模块调用；所有原本成熟可用的业务实现全部保留。**

---

## 附：给 Codex 的一句话摘要

> 把 `front-api + content-rpc + interaction-rpc + user-rpc + count-rpc` 合并成一个 `ranfeed-app` 单体应用；保留 content/feed/interaction/user/count 五个领域模块；删除 gRPC、Etcd、zRPC 与服务注册发现；把所有跨服务调用改为 `ServiceContext` 中的模块方法调用；Redis、Lua、MySQL、Kafka、Canal、OSS、XXL-Job、OTel、Prometheus、ELK 全部继续保留。

