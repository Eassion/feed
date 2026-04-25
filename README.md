# ran-feed

Go 单体 FEED 项目后端脚手架，保留 `Handler -> Service -> Repository` 分层，并预埋 JWT、中间件、MySQL、Redis、Kafka 消费者基础骨架。

## 目录

```text
cmd/server
internal/cache
internal/config
internal/handler
internal/middleware
internal/model
internal/mq
internal/repository
internal/service
internal/svc
pkg/jwtutil
config
deploy
docs
script
```

## 快速启动

1. 修改 `config/config.yaml` 中的服务与依赖配置。
2. 运行 `go mod tidy` 拉取依赖。
3. 启动服务：`go run ./cmd/server`

## 默认接口

- `GET /healthz`
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `GET /api/v1/users/me`
- `PUT /api/v1/users/me`
- `POST /api/v1/users/avatar/upload`
- `POST /api/v1/contents/articles`
- `POST /api/v1/contents/videos`
- `POST /api/v1/contents/upload-credentials`
- `GET /api/v1/contents/{id}`
- `GET /api/v1/contents/{id}/comments`
- `DELETE /api/v1/contents/{id}`
- `POST /api/v1/uploads/objects`
- `GET /api/v1/assets/{objectKey...}`
- `GET /api/v1/users/{userID}/contents`
- `POST /api/v1/interactions/likes`
- `DELETE /api/v1/interactions/likes/{contentID}`
- `POST /api/v1/interactions/favorites`
- `DELETE /api/v1/interactions/favorites/{contentID}`
- `POST /api/v1/comments`
- `DELETE /api/v1/comments/{commentID}`
- `GET /api/v1/comments/{commentID}/replies`
- `POST /api/v1/follows`
- `DELETE /api/v1/follows/{followeeID}`
- `GET /api/v1/feed/recommend`，支持匿名访问；登录后可携带 `Authorization: Bearer <token>`
- `GET /api/v1/feed/following`
