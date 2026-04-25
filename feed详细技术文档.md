# 4. 表结构设计
## 4.1 总体表清单
业务库 `ran-feed` 的核心表：

1. `ran_feed_user`
2. `ran_feed_content`
3. `ran_feed_article`
4. `ran_feed_video`
5. `ran_feed_like`
6. `ran_feed_favorite`
7. `ran_feed_comment`
8. `ran_feed_follow`
9. `ran_feed_count_value`
10. `ran_feed_mq_consume_dedup`

建库和授权在 `bootstrap/init.sql` 中定义，且专门给了 Canal 所需的复制权限 

---

## 4.2 用户域
### `ran_feed_user`
用户主表，字段包括：

+ `id`
+ `username`
+ `nickname`
+ `avatar`
+ `bio`
+ `mobile`
+ `email`
+ `password_hash`
+ `password_salt`
+ `gender`
+ `birthday`
+ `status`
+ `version`
+ `is_deleted`
+ `created_at`
+ `updated_at`

并有唯一索引：

+ `uk_username`
+ `uk_mobile`
+ `uk_email`

见建表 SQL 

---

## 4.3 内容域
### `ran_feed_content`
统一内容主表，承载文章/视频公共字段：

+ `id`
+ `user_id`
+ `content_type`（10=文章，20=视频） 
+ `status`
+ `visibility`
+ `like_count`
+ `favorite_count`
+ `comment_count`
+ `hot_score`
+ `last_hot_score_at`
+ `version`
+ `is_deleted`
+ `published_at`

同时建了热榜、发布时间、作者维度索引：

+ `idx_ran_feed_content_hot_score`
+ `idx_ran_feed_content_user_created`
+ `idx_ran_feed_content_published`
+ `idx_ran_feed_content_user_published`

见 SQL 

### `ran_feed_article`
文章明细表，一对一挂在 `content_id` 上：

+ `content_id` 唯一 
+ `title`
+ `description`
+ `cover`
+ `content`

见 SQL 

### `ran_feed_video`
视频明细表：

+ `content_id`
+ `title`
+ `origin_url`
+ `hls_url`
+ `cover_url`
+ `duration`
+ `transcode_status`
+ `fail_reason`

见 SQL 

---

## 4.4 互动域
### `ran_feed_like`
点赞关系表：

+ `user_id`
+ `content_id`
+ `content_user_id`
+ `status`
+ `version`
+ `is_deleted`

唯一键 `(user_id, content_id)`，见 SQL 

### `ran_feed_favorite`
收藏关系表：

+ `user_id`
+ `content_id`
+ `content_user_id`
+ `status`

唯一键 `(user_id, content_id)`，见 SQL 

### `ran_feed_comment`
评论表：

+ `content_id`
+ `content_user_id`
+ `user_id`
+ `reply_to_user_id`
+ `parent_id`
+ `root_id`
+ `comment`
+ `status`
+ `is_deleted`

支持一级评论和楼中回复，见 SQL 

### `ran_feed_follow`
关注关系表：

+ `user_id`
+ `follow_user_id`
+ `status`
+ `is_deleted`

唯一键 `(user_id, follow_user_id)`，见 SQL 

### `ran_feed_mq_consume_dedup`
MQ 幂等去重表：

+ `consumer`
+ `event_id`
+ `created_at`

唯一键 `(consumer, event_id)`，见 SQL 

---

## 4.5 计数域
### `ran_feed_count_value`
统一计数表：

+ `biz_type`：10=like，20=favorite，30=comment，40=followed，41=following 
+ `target_type`：10=content，20=user 
+ `target_id`
+ `value`
+ `owner_id`

唯一键 `(biz_type, target_type, target_id)`，见 SQL 

这张表是全项目的**计数真相表**。

---

## 4.6 表关系摘要
逻辑外键关系是：

+ `ran_feed_content.user_id -> ran_feed_user.id`
+ `ran_feed_article.content_id -> ran_feed_content.id`
+ `ran_feed_video.content_id -> ran_feed_content.id`
+ `ran_feed_like.content_id -> ran_feed_content.id`
+ `ran_feed_like.user_id -> ran_feed_user.id`
+ `ran_feed_favorite.content_id -> ran_feed_content.id`
+ `ran_feed_comment.content_id -> ran_feed_content.id`
+ `ran_feed_comment.parent_id/root_id -> ran_feed_comment.id`
+ `ran_feed_follow.user_id -> ran_feed_user.id`
+ `ran_feed_follow.follow_user_id -> ran_feed_user.id`
+ `ran_feed_count_value.target_id` 按 `target_type` 指向内容或用户 

数据库层基本没有显式 `FOREIGN KEY`，更偏向应用层维护一致性。

---

# 5. Redis 设计
## 5.1 用户登录态
用户服务定义的 Redis key：

+ `user:session:{token}`
+ `user:session:user:{userId}`

默认过期 7 天，见 `user` 模块 Redis 常量和 session helper 

---

## 5.2 内容/Feed 相关
内容服务定义的 Redis key：

+  热榜索引：`feed:hot:global`
+  最新快照：`feed:hot:global:latest`
+  热榜快照：`feed:hot:global:snap:{snapshot_id}`
+  用户热榜快照映射：`feed:hot:global:user:{user_id}`
+  热榜增量分片：`feed:hot:global:inc:{shard}`
+  热榜快速更新锁：`feed:hot:global:lock:fast:{bucket}`
+  热榜冷更新锁：`feed:hot:global:lock:cold:{date}`
+  关注收件箱：`feed:follow:inbox:{user_id}`
+  关注收件箱重建锁：`feed:follow:inbox:lock:{user_id}`
+  用户发布列表：`feed:user:publish:{user_id}`
+  用户收藏列表：`feed:user:favorite:{user_id}`
+  用户收藏列表锁：`feed:user:favorite:lock:{user_id}`

见 Redis 常量定义 

---

## 5.3 互动相关
互动服务定义的 Redis key：

+  用户点赞热区：`like:user:{user_id}`
+  点赞计数：`like:count:{scene}:{content_id}`
+  收藏关系缓存：`favorite:rel:{scene}:{user_id}:{content_id}`
+  收藏列表：`feed:user:favorite:{user_id}`
+  评论对象：`comment:obj:{comment_id}`
+  一级评论索引：`comment:idx:content:{content_id}`
+  回复索引：`comment:idx:root:{root_id}`

见互动 Redis 常量文件 

---

## 5.4 计数相关
计数服务定义的 Redis key：

+  计数缓存：`count:value:{biz_type}:{target_type}:{target_id}`
+  计数重建锁：`lock:rebuild:count:{biz_type}:{target_type}:{target_id}`
+  用户主页计数缓存：`count:user:profile:{user_id}`
+  用户主页计数重建锁：`lock:rebuild:count:user:profile:{user_id}`
+  热榜增量分片：`feed:hot:global:inc:{shard}`

见计数 Redis 常量文件 

---

# 6. 功能清单与“去微服务后”的处理链路
下面所有链路都按“**单体版**”来描述，也就是把原来跨 RPC 的调用改成模块内调用，但保留原业务处理方式。

---

## 6.1 用户注册
### 功能
手机号注册，创建用户并自动登录。

### 处理链路
HTTP `/v1/users` → `userService.Register`

当前仓库里，front 只是透传给 user-rpc   
去微服务后，链路应当直接变成：

1.  校验手机号是否已存在：查 `ran_feed_user`
2.  生成 `password_salt` 和 `password_hash`
3.  写入 `ran_feed_user`
4.  生成 session token 
5.  写入 Redis： 
    - `user:session:{token}`
    - `user:session:user:{userId}`
6.  返回 token 和过期时间 

关键组件：

+  MySQL：`ran_feed_user`
+  Redis：登录态双 key 
+  密码哈希工具 
+  无需 Kafka/Canal 

参考实现见注册逻辑和 session helper 

---

## 6.2 用户登录
### 功能
手机号+密码登录。

### 链路
HTTP `/v1/login` → `userService.Login`

1.  查 `ran_feed_user` 按手机号取用户 
2.  校验密码哈希 
3.  生成 session token 
4.  Redis 写： 
    - `user:session:{token}`
    - `user:session:user:{userId}`
5.  返回 token 

参考实现见 front login 与 user login 逻辑 

---

## 6.3 退出登录
### 链路
HTTP `/v1/logout` → `userService.Logout`

1.  从上下文取 token 和 userId 
2.  删除 Redis 会话映射： 
    - `user:session:{token}`
    - `user:session:user:{userId}`

见注销逻辑和 session helper 

---

## 6.4 头像上传
### 功能
前端直接上传头像文件。

### 链路
HTTP `/v1/users/avatar/upload` → `UploadAvatarLogic`

1.  限制 multipart 大小 
2.  读取文件，校验 mime 类型 
3.  生成对象 key：`avatar/{date}/{timestamp_random}.{ext}`
4.  调 OSS 上传 
5.  返回 URL、ObjectKey、Mime、Size 

这里不经过 MySQL，不经过 Redis。

见头像上传逻辑 

---

## 6.5 内容上传凭证
### 功能
为前端直传文章封面/视频等生成 OSS 表单凭证。

### 链路
HTTP `/v1/content/upload-credentials` → `contentService.Uploads`

1.  解析上传场景和文件扩展名 
2.  构造 `UploadPolicy`
3.  由 OSS 策略生成临时上传表单 
4.  返回 `objectKey + formData + expiredAt`

见 front 逻辑与 content 上传凭证逻辑 

---

## 6.6 发布文章
### 功能
创建公开或私密文章。

### 链路
HTTP `/v1/content/article/publish` → `contentService.PublishArticle`

1.  MySQL 事务内写： 
    - `ran_feed_content`
    - `ran_feed_article`
2.  更新 Redis 用户发布列表 zset： 
    - `feed:user:publish:{user_id}`
    -  member=`content_id`
    -  score=`content_id`
3.  如果是公开内容，给热榜增量分片打“发布种子分”： 
    - `feed:hot:global:inc:{content_id % 64}`
    - `HINCRBYFLOAT field=content_id delta=2.4`

见文章发布逻辑和热榜种子 helper 

### 组件
+  MySQL 
+  Redis zset 
+  Redis hash（热榜增量） 
+  OSS 已在上传凭证阶段使用 

---

## 6.7 发布视频
### 功能
创建视频内容。

### 链路
与文章发布类似，只是明细表换成 `ran_feed_video`，并写入：

+ `origin_url`
+ `cover_url`
+ `duration`
+ `transcode_status=10`

同时也更新：

+ `feed:user:publish:{user_id}`
+ `feed:hot:global:inc:{shard}`

见视频发布逻辑 

---

## 6.8 删除内容
### 功能
作者删除自己的内容。

### 链路
HTTP `DELETE /v1/content/:content_id` → `contentService.DeleteContent`

1.  查 `ran_feed_content` 判断存在且作者本人 
2.  事务删除： 
    - `ran_feed_article` 或 `ran_feed_video`
    - `ran_feed_content`
3.  Redis 清理： 
    - `feed:user:publish:{user_id}` 里 `ZREM content_id`
    - `feed:hot:global` 里 `ZREM content_id`

见删除逻辑 

### 注意
项目当前数据库层看起来是物理删除/软删除混合，内容删除逻辑本身是调 repo 删除；但 Canal 侧还监听 `ran_feed_content` 删除状态变化，用于把对应内容的点赞/收藏/评论计数清零 

---

## 6.9 内容详情
### 功能
查看文章/视频详情页。

### 链路
HTTP `/v1/content/detail` → `contentService.GetContentDetail`

1.  查 `ran_feed_content`
2.  根据 `content_type` 查： 
    - `ran_feed_article` 或 
    - `ran_feed_video`
3.  并行查询： 
    -  作者信息：用户模块 
    -  是否已点赞：互动 like 
    -  是否已收藏：互动 favorite 
    -  是否关注作者：互动 follow 
    -  三类计数：count 模块 `like/favorite/comment`
4.  组装详情返回 

见内容详情逻辑 

### 组件
+  MySQL：内容主表+明细表 
+  Count：`count:value:*`
+  互动关系缓存： 
    -  点赞热区 
    -  收藏关系缓存 
+  用户信息表 

---

## 6.10 推荐流
### 功能
按热榜返回推荐内容。

### 链路
HTTP `/v1/feed/recommend` → `feedService.RecommendFeed`

1.  从 Redis 热榜快照中按 cursor 取内容 id： 
    -  优先指定快照：`feed:hot:global:snap:{snapshot_id}`
    -  否则用 `feed:hot:global:latest` 找当前快照 
    -  如果都没有，则回退到 `feed:hot:global`
2.  再批量查 MySQL： 
    - `ran_feed_content`
    - `ran_feed_article`
    - `ran_feed_video`
3.  并行补充： 
    -  用户资料 
    -  点赞状态与点赞数 
4.  返回推荐流 items 

见推荐逻辑与热榜 Redis key 定义 

### 组件
+  Redis zset / snapshot 
+  MySQL 内容表 
+  用户模块 
+  互动 like 模块 

---

## 6.11 关注流
### 功能
查看“我关注的人发了什么”。

### 链路
HTTP `/v1/feed/follow` → `feedService.FollowFeed`

1.  先查 Redis 收件箱： 
    - `feed:follow:inbox:{user_id}`
2.  若缓存不存在，则尝试抢重建锁： 
    - `feed:follow:inbox:lock:{user_id}`
3.  抢到锁后： 
    -  拉取我的 followee 列表 
    -  查这些作者发布的内容 
    -  回填 `feed:follow:inbox:{user_id}`
4.  再按 cursor 从 inbox zset 取当前页内容 
5.  补充作者信息、点赞状态和点赞数 

见关注流逻辑 

### 组件
+  Redis zset：关注收件箱 
+  Redis 分布式锁 
+  MySQL 内容表 
+  Follow 关系模块 
+  Like 模块 
+  User 模块 

---

## 6.12 关注用户
### 功能
关注一个用户。

### 链路
HTTP `/v1/interaction/followings` → `interactionService.FollowUser`

1. `ran_feed_follow` 做 upsert 
2.  异步触发“关注收件箱回填” 
    -  读取被关注者的 `feed:user:publish:{followee_id}`
    -  取最近 N 条 
    -  回填到 `feed:follow:inbox:{follower_id}`

见 follow 逻辑和回填逻辑 

### 组件
+  MySQL：`ran_feed_follow`
+  Redis zset： 
    - `feed:user:publish:{followee}`
    - `feed:follow:inbox:{follower}`

---

## 6.13 用户发布列表
### 功能
查看某个作者发布过的内容。

### 链路
HTTP `/v1/feed/user/publish` → `feedService.UserPublishFeed`

1.  先查 Redis： 
    - `feed:user:publish:{author_id}`
2.  如果缓存不存在： 
    -  抢锁 `feed:user:publish:lock:{author_id}`（代码里是字符串拼出来的） 
    -  回源 MySQL 查作者所有已发布公开内容 
    -  回填 zset 
3.  按 zset cursor 取一页 
4.  批量查内容明细 
5.  并行补作者信息、点赞状态和点赞数 

见用户发布流逻辑 

### 组件
+  Redis zset + rebuild lock 
+  MySQL 内容表 
+  User 模块 
+  Like 模块 

---

## 6.14 用户收藏列表
### 功能
查看某个用户收藏过的内容。

### 链路
HTTP `/v1/feed/user/favorite` → `feedService.UserFavoriteFeed`

1.  先查 Redis： 
    - `feed:user:favorite:{user_id}`
2.  缓存缺失则： 
    -  抢锁 `feed:user:favorite:lock:{user_id}`
    -  分页拉取 `FavoriteRpc.QueryFavoriteList`
    -  回填 zset，score 用 `favorite_id`
3.  根据 zset 里的 content_id 批量查内容 
4.  再复用发布流的组装逻辑补： 
    -  作者信息 
    -  点赞状态 
    -  点赞数 

见收藏流逻辑 

### 组件
+  Redis zset + lock 
+  MySQL 收藏表 
+  MySQL 内容表 
+  Like/User 模块 

---

## 6.15 点赞
### 功能
给内容点赞。

### 链路
HTTP `/v1/interaction/like` → `interactionService.Like`

1.  Redis Lua 直接在用户维度热区里处理点赞： 
    -  key=`like:user:{user_id}`
2.  如果状态发生变化，则异步发 Kafka 点赞事件 
3.  下游消费者落库并维护计数 

见点赞逻辑与 interaction service context 

### 组件
+  Redis hash：`like:user:{user_id}`
+  Kafka producer 
+  MySQL 落库在消费端 
+  Count 聚合链路 

### 说明
这个项目的点赞不是“先写 MySQL 再删缓存”，而是**先走 Redis + MQ 事件**，设计上更偏高频互动优化。

---

## 6.16 取消点赞
仓库里有 `unlike` 前端路由，逻辑与点赞对称，核心仍是：

+  操作 `like:user:{user_id}`
+  异步发 Kafka 事件 
+  下游统一落库/改计数 

前端路由可见 

---

## 6.17 收藏
### 功能
收藏内容。

### 链路
HTTP `/v1/interaction/favorite` → `interactionService.Favorite`

1.  MySQL `ran_feed_favorite` upsert 
2.  删除收藏关系缓存： 
    - `favorite:rel:{scene}:{user_id}:{content_id}`
3.  如果用户收藏列表缓存存在，则增量追加： 
    - `feed:user:favorite:{user_id}`

见收藏逻辑与 Redis 常量 

### 组件
+  MySQL：`ran_feed_favorite`
+  Redis string：收藏关系缓存 
+  Redis zset：收藏列表 

---

## 6.18 取消收藏
前端有 `DELETE /v1/interaction/favorite` 路由   
虽然这里没展开具体实现文件，但按当前设计应与“收藏”对称：

+  更新 `ran_feed_favorite.status`
+  删除 `favorite:rel:*`
+  从 `feed:user:favorite:{user_id}` 中移除对应 content 

---

## 6.19 评论
### 功能
一级评论和回复评论。

### 链路
HTTP `/v1/interaction/comment` → `interactionService.Comment`

1.  校验 parent/root/reply_to 关系 
2.  MySQL 插入 `ran_feed_comment`
3.  如果是一级评论： 
    -  写评论对象缓存：`comment:obj:{comment_id}`
    -  更新一级评论索引：`comment:idx:content:{content_id}`
4.  如果是回复： 
    -  删除父评论对象缓存 
    -  删除根评论对象缓存 
    -  删除回复索引 `comment:idx:root:{root_id}`
    -  让下次读取回源重建 

见评论逻辑与 Redis 常量 

### 组件
+  MySQL：`ran_feed_comment`
+  Redis hash/string：评论对象 
+  Redis zset/index：评论索引 
+  User 模块：补评论用户昵称头像 

---

## 6.20 删除评论
前端存在 `DELETE /v1/interaction/comment` 路由   
按当前缓存设计，删除评论应当：

+  更新 `ran_feed_comment.status/is_deleted`
+  删除相关 `comment:obj:*`
+  删除/失效相关 `comment:idx:content:*` 或 `comment:idx:root:*`
+  由下次查询回源重建 

---

## 6.21 用户主页
### 功能
查看用户资料、作品数、关注数、粉丝数、获赞数、被收藏数。

### 链路
HTTP `/v1/user/profile/:userId`

当前 front 逻辑并行查三类信息：

1. `UserRpc.GetUserProfile`
2. `CountRpc.GetUserProfileCounts`
3. `ContentRpc.GetUserContentCount`

见前端主页逻辑 

去微服务后，应该变成模块内并行：

1.  用户基本信息：查 `ran_feed_user`
2.  作品数：查 `ran_feed_content` 已发布公开内容数量 
3.  用户主页计数： 
    -  先查 Redis `count:user:profile:{user_id}`
    -  miss 则抢锁 `lock:rebuild:count:user:profile:{user_id}`
    -  DB 回源： 
        * `following/followed` 从 `ran_feed_count_value`
        *  获赞/被收藏总量通过 `owner_id` 聚合 
    -  回填缓存 

见主页计数逻辑和作品数逻辑 

### 组件
+  MySQL：`ran_feed_user`, `ran_feed_content`, `ran_feed_count_value`
+  Redis： 
    - `count:user:profile:{user_id}`
    - `lock:rebuild:count:user:profile:{user_id}`

### 备注
仓库这里有个明显问题：前端在查 `GetUserProfileCounts` 时传的是 `viewerID`，不是页面上的 `userID`，这会导致主页计数可能查错人   
另外 `followResp` 变量声明了但没有真正并行赋值，当前主页“是否关注”很可能没接完。

---

# 7. 计数与热榜链路
这部分是项目里最有特色的一段。

## 7.1 Canal + Kafka + Count 聚合
Canal 被配置成监听这些表：

+ `ran_feed_favorite`
+ `ran_feed_comment`
+ `ran_feed_like`
+ `ran_feed_follow`
+ `ran_feed_content`

并把变更发到 Kafka topic `ran-feed-count-canal`

`count-rpc` 启动时会拉起 Canal Kafka consumer 

consumer 会：

1.  解析 Canal flat message 
2.  根据表名走 strategy 
3.  做消息幂等： 
    - `ran_feed_mq_consume_dedup`
4.  更新 `ran_feed_count_value`
5.  失效 `count:value:*`
6.  失效 `count:user:profile:*`
7.  把互动增量写入热榜分片： 
    - `feed:hot:global:inc:{shard}`

见 `canal_count_consumer.go` 和各 strategy 文件 

## 7.2 各表如何映射成计数
+ `ran_feed_like` → 内容点赞数 
+ `ran_feed_favorite` → 内容收藏数 
+ `ran_feed_comment` → 内容评论数 
+ `ran_feed_follow` → 用户关注数 + 粉丝数 
+ `ran_feed_content` 删除 → 内容相关 like/favorite/comment 清零 

见策略注册和具体策略文件 

---

# 8. 推荐榜更新链路
项目里 XXL-Job 初始化 SQL 里明确注册了两个任务：

+ `hot.fast.update`
+ `hot.cold.update`

分别叫“热更新推荐榜”和“冷更新推荐榜” 

虽然我这次没有继续把两个 handler 文件完全展开，但从仓库里已经能确认这套热榜数据结构：

+  主热榜：`feed:hot:global`
+  最新快照：`feed:hot:global:latest`
+  快照：`feed:hot:global:snap:{snapshot_id}`
+  增量分片：`feed:hot:global:inc:{shard}`
+  快速更新锁：`feed:hot:global:lock:fast:{bucket}`
+  冷更新锁：`feed:hot:global:lock:cold:{date}`

见内容 Redis 常量 

再结合：

+  发布内容会给 `feed:hot:global:inc:{shard}` 打 2.4 的种子热度 
+  Canal 计数消费者会把点赞/评论/收藏行为也累计到 `feed:hot:global:inc:{shard}`

可以确定推荐榜链路是：

1.  发布和互动行为把增量写进 `feed:hot:global:inc:*`
2.  XXL-Job 的 fast/cold handler 定期把增量合并成热榜/快照 
3.  推荐流从快照读 

