package feedrepo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"feed/internal/cache"
	"feed/internal/model"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Repository struct {
	db    *gorm.DB
	redis *redis.Client
	inbox *cache.InboxStore
}

const feedOverscan = 32

type RecommendQuery struct {
	SnapshotID string
	Cursor     string
	Limit      int
}

type FollowQuery struct {
	UserID int64
	Cursor string
	Limit  int
}

type UserPublishQuery struct {
	AuthorID int64
	Cursor   string
	Limit    int
}

type UserFavoriteQuery struct {
	UserID int64
	Cursor string
	Limit  int
}

type FeedPage struct {
	Items      []model.FeedItem
	NextCursor string
	HasMore    bool
	SnapshotID string
}

func New(db *gorm.DB, redisClient *redis.Client) *Repository {
	return &Repository{
		db:    db,
		redis: redisClient,
		inbox: cache.NewInboxStore(redisClient, cache.FollowInboxKeepN),
	}
}

// ListRecommended 获取推荐流内容
func (r *Repository) ListRecommended(ctx context.Context, query RecommendQuery) (*FeedPage, error) {
	if query.Limit <= 0 {
		query.Limit = 20
	}

	if r.redis != nil {
		//从缓存源列表中遍历读取
		//支持基于snapshotID和cursor的分页
		for _, source := range r.resolveSnapshotSources(ctx, query.SnapshotID) {
			ids, scoreMap, nextCursor, hasMore, err := r.listZSetIDsByCursor(ctx, source.key, query.Cursor, query.Limit)
			if err != nil {
				return nil, err
			}
			if len(ids) == 0 {
				continue
			}

			items, err := r.loadFeedItems(ctx, ids, scoreMap)
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				continue
			}

			return &FeedPage{
				Items:      items,
				NextCursor: nextCursor,
				HasMore:    hasMore,
				SnapshotID: source.snapshotID,
			}, nil
		}
	}

	snapshotID := query.SnapshotID
	if snapshotID == "" {
		snapshotID = "global"
	}

	if r.db == nil {
		return &FeedPage{
			Items:      []model.FeedItem{},
			NextCursor: "",
			HasMore:    false,
			SnapshotID: snapshotID,
		}, nil
	}

	var records []feedRecord
	dbQuery := r.db.WithContext(ctx).
		Model(&feedRecord{}).
		Where("status = ? AND is_deleted = ? AND visibility = ?", model.ContentStatusPublished, false, model.ContentVisibilityPublic)
	if cursorScore, cursorID, ok := parseCursor(query.Cursor); ok {
		dbQuery = dbQuery.Where("hot_score < ? OR (hot_score = ? AND id < ?)", cursorScore, cursorScore, cursorID)
	}
	if err := dbQuery.
		Order("hot_score DESC, id DESC").
		Limit(query.Limit + 1).
		Find(&records).Error; err != nil {
		return nil, err
	}

	hasMore := len(records) > query.Limit
	if hasMore {
		records = records[:query.Limit]
	}

	items := make([]model.FeedItem, 0, query.Limit)
	for _, record := range records {
		items = append(items, model.FeedItem{
			ID:          record.ID,
			AuthorID:    record.AuthorID,
			ContentType: model.ContentTypeName(record.ContentType),
			Title:       fmt.Sprintf("content-%d", record.ID),
			Summary:     "feed item loaded from MySQL",
			Score:       record.HotScore,
			CreatedAt:   record.PublishedAt,
		})
	}

	return &FeedPage{
		Items:      items,
		NextCursor: buildNextCursor(items),
		HasMore:    hasMore,
		SnapshotID: snapshotID,
	}, nil
}

// ListFollowing 获取关注流内容
func (r *Repository) ListFollowing(ctx context.Context, query FollowQuery) (*FeedPage, error) {
	if query.UserID <= 0 {
		return &FeedPage{Items: []model.FeedItem{}}, nil
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}

	//和关注流复用的同一个函数  从缓存查
	ids, scoreMap, nextCursor, hasMore, err := r.listZSetIDsByCursor(ctx, cache.FollowInboxKey(query.UserID), query.Cursor, query.Limit)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &FeedPage{
			Items:      []model.FeedItem{},
			NextCursor: "",
			HasMore:    false,
			SnapshotID: "",
		}, nil
	}

	//聚合title和summary信息，基于内容ID列表和scoreMap构建feed item列表
	items, err := r.loadFeedItems(ctx, ids, scoreMap)
	if err != nil {
		return nil, err
	}

	return &FeedPage{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (r *Repository) ListUserPublished(ctx context.Context, query UserPublishQuery) (*FeedPage, error) {
	if query.AuthorID <= 0 {
		return &FeedPage{Items: []model.FeedItem{}}, nil
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}

	if r.redis != nil {
		ids, scoreMap, nextCursor, hasMore, err := r.listZSetIDsByCursor(ctx, cache.UserPublishKey(query.AuthorID), query.Cursor, query.Limit)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			exists, err := r.UserPublishExists(ctx, query.AuthorID)
			if err != nil {
				return nil, err
			}
			if exists {
				return &FeedPage{
					Items:      []model.FeedItem{},
					NextCursor: "",
					HasMore:    false,
				}, nil
			}
		}
		if len(ids) > 0 {
			items, err := r.loadFeedItems(ctx, ids, scoreMap)
			if err != nil {
				return nil, err
			}
			if len(items) > 0 {
				return &FeedPage{
					Items:      items,
					NextCursor: nextCursor,
					HasMore:    hasMore,
				}, nil
			}
		}
	}

	if r.db == nil {
		return &FeedPage{
			Items:      []model.FeedItem{},
			NextCursor: "",
			HasMore:    false,
		}, nil
	}

	var records []feedRecord
	dbQuery := r.db.WithContext(ctx).
		Model(&feedRecord{}).
		Where("user_id = ? AND status = ? AND is_deleted = ? AND visibility = ?", query.AuthorID, model.ContentStatusPublished, false, model.ContentVisibilityPublic)
	if cursorScore, cursorID, ok := parseCursor(query.Cursor); ok {
		cursorTime := time.UnixMilli(int64(cursorScore))
		dbQuery = dbQuery.Where("published_at < ? OR (published_at = ? AND id < ?)", cursorTime, cursorTime, cursorID)
	}
	if err := dbQuery.
		Order("published_at DESC, id DESC").
		Limit(query.Limit + 1).
		Find(&records).Error; err != nil {
		return nil, err
	}

	hasMore := len(records) > query.Limit
	if hasMore {
		records = records[:query.Limit]
	}

	ids := make([]int64, 0, len(records))
	scoreMap := make(map[int64]float64, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
		scoreMap[record.ID] = float64(record.PublishedAt.UnixMilli())
	}

	items, err := r.loadFeedItems(ctx, ids, scoreMap)
	if err != nil {
		return nil, err
	}

	return &FeedPage{
		Items:      items,
		NextCursor: buildNextCursor(items),
		HasMore:    hasMore,
	}, nil
}

func (r *Repository) ListUserFavorited(ctx context.Context, query UserFavoriteQuery) (*FeedPage, error) {
	if query.UserID <= 0 {
		return &FeedPage{Items: []model.FeedItem{}}, nil
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}

	if r.redis != nil {
		ids, scoreMap, nextCursor, hasMore, err := r.listZSetIDsByCursor(ctx, cache.UserFavoriteKey(query.UserID), query.Cursor, query.Limit)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			exists, err := r.UserFavoriteExists(ctx, query.UserID)
			if err != nil {
				return nil, err
			}
			if exists {
				return &FeedPage{
					Items:      []model.FeedItem{},
					NextCursor: "",
					HasMore:    false,
				}, nil
			}
		}
		if len(ids) > 0 {
			items, err := r.loadFeedItems(ctx, ids, scoreMap)
			if err != nil {
				return nil, err
			}
			if len(items) > 0 {
				return &FeedPage{
					Items:      items,
					NextCursor: nextCursor,
					HasMore:    hasMore,
				}, nil
			}
		}
	}

	if r.db == nil {
		return &FeedPage{
			Items:      []model.FeedItem{},
			NextCursor: "",
			HasMore:    false,
		}, nil
	}

	var records []favoriteRecord
	dbQuery := r.db.WithContext(ctx).
		Model(&favoriteRecord{}).
		Where("user_id = ? AND status = ?", query.UserID, model.InteractionStatusActive)
	if cursorScore, cursorID, ok := parseCursor(query.Cursor); ok {
		cursorFavoriteID := int64(cursorScore)
		dbQuery = dbQuery.Where("id < ? OR (id = ? AND content_id < ?)", cursorFavoriteID, cursorFavoriteID, cursorID)
	}
	if err := dbQuery.
		Order("id DESC").
		Limit(query.Limit + 1).
		Find(&records).Error; err != nil {
		return nil, err
	}

	hasMore := len(records) > query.Limit
	if hasMore {
		records = records[:query.Limit]
	}

	ids := make([]int64, 0, len(records))
	scoreMap := make(map[int64]float64, len(records))
	for _, record := range records {
		ids = append(ids, record.ContentID)
		scoreMap[record.ContentID] = float64(record.ID)
	}

	items, err := r.loadFeedItems(ctx, ids, scoreMap)
	if err != nil {
		return nil, err
	}

	return &FeedPage{
		Items:      items,
		NextCursor: buildNextCursor(items),
		HasMore:    hasMore,
	}, nil
}

func (r *Repository) PushToFollowersInbox(ctx context.Context, followerIDs []int64, contentID int64, createdAt time.Time) error {
	if len(followerIDs) == 0 {
		return nil
	}

	score := float64(createdAt.UnixMilli())
	for _, followerID := range followerIDs {
		if err := r.inbox.AddContent(ctx, followerID, contentID, score); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) AddToInboxEntries(ctx context.Context, userID int64, entries []cache.InboxEntry) error {
	if r.redis == nil || userID <= 0 || len(entries) == 0 {
		return nil
	}

	return r.inbox.AddBatch(ctx, userID, entries)
}

func (r *Repository) AddToUserPublish(ctx context.Context, authorID, contentID int64, publishedAt time.Time) error {
	if r.redis == nil || authorID <= 0 || contentID <= 0 {
		return nil
	}
	if publishedAt.IsZero() {
		publishedAt = time.Now()
	}

	return r.redis.ZAdd(ctx, cache.UserPublishKey(authorID), redis.Z{
		Score:  float64(publishedAt.UnixMilli()),
		Member: strconv.FormatInt(contentID, 10),
	}).Err()
}

func (r *Repository) AddToUserFavorite(ctx context.Context, userID, contentID, favoriteID int64) error {
	if r.redis == nil || userID <= 0 || contentID <= 0 || favoriteID <= 0 {
		return nil
	}

	return r.redis.ZAdd(ctx, cache.UserFavoriteKey(userID), redis.Z{
		Score:  float64(favoriteID),
		Member: strconv.FormatInt(contentID, 10),
	}).Err()
}

func (r *Repository) RemoveFromUserPublish(ctx context.Context, authorID, contentID int64) error {
	if r.redis == nil || authorID <= 0 || contentID <= 0 {
		return nil
	}

	return r.redis.ZRem(ctx, cache.UserPublishKey(authorID), strconv.FormatInt(contentID, 10)).Err()
}

func (r *Repository) RemoveFromUserFavorite(ctx context.Context, userID, contentID int64) error {
	if r.redis == nil || userID <= 0 || contentID <= 0 {
		return nil
	}

	return r.redis.ZRem(ctx, cache.UserFavoriteKey(userID), strconv.FormatInt(contentID, 10)).Err()
}

func (r *Repository) InboxExists(ctx context.Context, userID int64) (bool, error) {
	return r.inbox.Exists(ctx, userID)
}

func (r *Repository) TryAcquireInboxRebuildLock(ctx context.Context, userID int64) (bool, error) {
	return r.inbox.TryAcquireRebuildLock(ctx, userID)
}

func (r *Repository) RebuildInbox(ctx context.Context, userID int64, contents []model.Content) error {
	entries := make([]cache.InboxEntry, 0, len(contents))
	for _, item := range contents {
		publishedAt := item.PublishedAt
		if publishedAt.IsZero() {
			publishedAt = item.CreatedAt
		}
		entries = append(entries, cache.InboxEntry{
			ContentID: item.ID,
			Score:     float64(publishedAt.UnixMilli()),
		})
	}

	return r.inbox.Replace(ctx, userID, entries)
}

func (r *Repository) ListUserPublishEntries(ctx context.Context, authorID int64, limit int64) ([]cache.InboxEntry, error) {
	if r.redis == nil || authorID <= 0 || limit <= 0 {
		return []cache.InboxEntry{}, nil
	}

	zItems, err := r.redis.ZRevRangeWithScores(ctx, cache.UserPublishKey(authorID), 0, limit-1).Result()
	if err != nil {
		if err == redis.Nil {
			return []cache.InboxEntry{}, nil
		}
		return nil, err
	}

	entries := make([]cache.InboxEntry, 0, len(zItems))
	for _, item := range zItems {
		contentID, ok := parseMemberID(item.Member)
		if !ok {
			continue
		}
		entries = append(entries, cache.InboxEntry{
			ContentID: contentID,
			Score:     item.Score,
		})
	}

	return entries, nil
}

func (r *Repository) UserPublishExists(ctx context.Context, authorID int64) (bool, error) {
	if r.redis == nil || authorID <= 0 {
		return false, nil
	}

	exists, err := r.redis.Exists(ctx, cache.UserPublishInitKey(authorID), cache.UserPublishKey(authorID)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

func (r *Repository) TryAcquireUserPublishRebuildLock(ctx context.Context, authorID int64) (bool, error) {
	if r.redis == nil || authorID <= 0 {
		return true, nil
	}

	return r.redis.SetNX(ctx, cache.UserPublishRebuildLockKey(authorID), "1", cache.UserPublishRebuildLockTTL).Result()
}

func (r *Repository) RebuildUserPublish(ctx context.Context, authorID int64, contents []model.Content) error {
	if r.redis == nil || authorID <= 0 {
		return nil
	}

	key := cache.UserPublishKey(authorID)
	pipe := r.redis.TxPipeline()
	pipe.Del(ctx, key)
	pipe.Set(ctx, cache.UserPublishInitKey(authorID), "1", 0)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	if len(contents) == 0 {
		return nil
	}

	members := make([]redis.Z, 0, len(contents))
	for _, item := range contents {
		publishedAt := item.PublishedAt
		if publishedAt.IsZero() {
			publishedAt = item.CreatedAt
		}
		members = append(members, redis.Z{
			Score:  float64(publishedAt.UnixMilli()),
			Member: strconv.FormatInt(item.ID, 10),
		})
	}

	return r.redis.ZAdd(ctx, key, members...).Err()
}

func (r *Repository) UserFavoriteExists(ctx context.Context, userID int64) (bool, error) {
	if r.redis == nil || userID <= 0 {
		return false, nil
	}

	exists, err := r.redis.Exists(ctx, cache.UserFavoriteInitKey(userID), cache.UserFavoriteKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

func (r *Repository) TryAcquireUserFavoriteRebuildLock(ctx context.Context, userID int64) (bool, error) {
	if r.redis == nil || userID <= 0 {
		return true, nil
	}

	return r.redis.SetNX(ctx, cache.UserFavoriteRebuildLockKey(userID), "1", cache.UserFavoriteRebuildLockTTL).Result()
}

func (r *Repository) RebuildUserFavorite(ctx context.Context, userID int64, favorites []model.Favorite) error {
	if r.redis == nil || userID <= 0 {
		return nil
	}

	key := cache.UserFavoriteKey(userID)
	pipe := r.redis.TxPipeline()
	pipe.Del(ctx, key)
	pipe.Set(ctx, cache.UserFavoriteInitKey(userID), "1", 0)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	if len(favorites) == 0 {
		return nil
	}

	members := make([]redis.Z, 0, len(favorites))
	for _, item := range favorites {
		members = append(members, redis.Z{
			Score:  float64(item.ID),
			Member: strconv.FormatInt(item.ContentID, 10),
		})
	}

	return r.redis.ZAdd(ctx, key, members...).Err()
}

func (r *Repository) InvalidateUserFavorite(ctx context.Context, userID int64) error {
	if r.redis == nil || userID <= 0 {
		return nil
	}

	return r.redis.Del(ctx, cache.UserFavoriteKey(userID), cache.UserFavoriteInitKey(userID)).Err()
}

// 从key缓存中按cursor查limit条数据，返回内容ID列表、ID对应的score、下一页cursor、是否有下一页hasMore
func (r *Repository) listZSetIDsByCursor(ctx context.Context, key, cursor string, limit int) ([]int64, map[int64]float64, string, bool, error) {
	if r.redis == nil {
		return nil, nil, "", false, nil
	}

	cursorScore, cursorID, hasCursor := parseCursor(cursor)
	//查询时加上一个feedOverscan，多查一些的目的是抵消后面在解析、游标过滤、内容可见性校验中被丢弃的损耗
	queryLimit := int64(limit + feedOverscan)
	var zItems []redis.Z
	var err error
	if hasCursor {
		zItems, err = r.redis.ZRevRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
			Max:    fmt.Sprintf("%.6f", cursorScore),
			Min:    "-inf",
			Offset: 0,
			Count:  queryLimit,
		}).Result()
	} else {
		zItems, err = r.redis.ZRevRangeWithScores(ctx, key, 0, queryLimit-1).Result()
	}
	if err != nil {
		if err == redis.Nil {
			return nil, nil, "", false, nil
		}
		return nil, nil, "", false, err
	}
	if len(zItems) == 0 {
		return nil, nil, "", false, nil
	}

	//limit+1的目的是判断是否有下一页，最终返回给用户的结果仍然是limit条
	filtered := make([]redis.Z, 0, limit+1)
	for _, item := range zItems {
		memberID, ok := parseMemberID(item.Member)
		if !ok {
			continue
		}
		//通过cursor过滤掉上一页最后一条数据以及相同score但id更小的数据，避免分页重复
		if hasCursor {
			if item.Score > cursorScore {
				continue
			}
			if item.Score == cursorScore && memberID >= cursorID {
				continue
			}
		}
		filtered = append(filtered, item)
		if len(filtered) >= limit+1 {
			break
		}
	}

	hasMore := len(filtered) > limit
	if hasMore {
		filtered = filtered[:limit]
	}

	ids := make([]int64, 0, len(filtered))
	scoreMap := make(map[int64]float64, len(filtered))
	items := make([]model.FeedItem, 0, len(filtered))
	for _, item := range filtered {
		memberID, ok := parseMemberID(item.Member)
		if !ok {
			continue
		}
		ids = append(ids, memberID)
		scoreMap[memberID] = item.Score
		items = append(items, model.FeedItem{ID: memberID, Score: item.Score})
	}

	return ids, scoreMap, buildNextCursor(items), hasMore, nil
}

//从数据库加载feed item详情，基于内容ID列表和scoreMap构建feed item列表
func (r *Repository) loadFeedItems(ctx context.Context, ids []int64, scoreMap map[int64]float64) ([]model.FeedItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	if r.db == nil {
		return []model.FeedItem{}, nil
	}

	var records []feedRecord
	//再次过滤一次，确保内容是已发布、未删除、公开可见的，避免缓存和数据库数据不一致导致的内容不可见问题
	if err := r.db.WithContext(ctx).
		Model(&feedRecord{}).
		Where("id IN ? AND status = ? AND is_deleted = ? AND visibility = ?", ids, model.ContentStatusPublished, false, model.ContentVisibilityPublic).
		Find(&records).Error; err != nil {
		return nil, err
	}

	recordMap := make(map[int64]feedRecord, len(records))
	for _, record := range records {
		recordMap[record.ID] = record
	}

	//从article或者video加载简要信息
	briefMap, err := r.loadBriefMap(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]model.FeedItem, 0, len(ids))
	for _, id := range ids {
		record, ok := recordMap[id]
		if !ok {
			continue
		}
		title := fmt.Sprintf("content-%d", record.ID)
		summary := "feed item loaded from MySQL"
		if brief, ok := briefMap[id]; ok {
			if brief.Title != "" {
				title = brief.Title
			}
			if brief.Summary != "" {
				summary = brief.Summary
			}
		}
		items = append(items, model.FeedItem{
			ID:            record.ID,
			AuthorID:      record.AuthorID,
			ContentType:   model.ContentTypeName(record.ContentType),
			Title:         title,
			Summary:       summary,
			Score:         scoreMap[id],
			LikeCount:     record.LikeCount,
			FavoriteCount: record.FavoriteCount,
			CommentCount:  record.CommentCount,
			CreatedAt:     record.PublishedAt,
		})
	}

	return items, nil
}

type feedRecord struct {
	ID            int64     `gorm:"column:id"`
	AuthorID      int64     `gorm:"column:user_id"`
	ContentType   int32     `gorm:"column:content_type"`
	Status        int32     `gorm:"column:status"`
	Visibility    string    `gorm:"column:visibility"`
	LikeCount     int64     `gorm:"column:like_count"`
	FavoriteCount int64     `gorm:"column:favorite_count"`
	CommentCount  int64     `gorm:"column:comment_count"`
	HotScore      float64   `gorm:"column:hot_score"`
	PublishedAt   time.Time `gorm:"column:published_at"`
	IsDeleted     bool      `gorm:"column:is_deleted"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

type favoriteRecord struct {
	ID        int64 `gorm:"column:id"`
	UserID    int64 `gorm:"column:user_id"`
	ContentID int64 `gorm:"column:content_id"`
	Status    int32 `gorm:"column:status"`
}

func (feedRecord) TableName() string {
	return "ran_feed_content"
}

func (favoriteRecord) TableName() string {
	return "ran_feed_favorite"
}

func parseCursor(cursor string) (float64, int64, bool) {
	if cursor == "" {
		return 0, 0, false
	}

	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}

	score, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, 0, false
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}

	return score, id, true
}

//解析content_id
func parseMemberID(member any) (int64, bool) {
	switch value := member.(type) {
	case string:
		id, err := strconv.ParseInt(value, 10, 64)
		return id, err == nil
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}

func buildNextCursor(items []model.FeedItem) string {
	if len(items) == 0 {
		return ""
	}

	last := items[len(items)-1]
	return fmt.Sprintf("%.6f:%d", last.Score, last.ID)
}

type snapshotSource struct {
	key        string
	snapshotID string
}

type feedBrief struct {
	Title   string
	Summary string
}

//构建一个快照源列表，优先使用preferredSnapshotID，如果没有则使用latestSnapshotID，
// 最后使用global作为兜底
func (r *Repository) resolveSnapshotSources(ctx context.Context, preferredSnapshotID string) []snapshotSource {
	sources := make([]snapshotSource, 0, 3)
	seen := make(map[string]struct{})

	appendSource := func(snapshotID string) {
		var key string
		var resolvedID string
		if snapshotID == "" || snapshotID == "global" {
			key = cache.FeedHotGlobalKey
			resolvedID = "global"
		} else {
			key = cache.FeedHotSnapshotKey(snapshotID)
			resolvedID = snapshotID
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		sources = append(sources, snapshotSource{key: key, snapshotID: resolvedID})
	}

	//添加用户指定snapshotID
	if preferredSnapshotID != "" {
		appendSource(preferredSnapshotID)
	}
	if r.redis != nil {
		if latestSnapshotID, err := r.redis.Get(ctx, cache.FeedHotLatestSnapshotKey).Result(); err == nil && latestSnapshotID != "" {
			//添加最新snapshotID
			appendSource(latestSnapshotID)
		}
	}
	//添加全局snapshotID
	appendSource("global")

	return sources
}

//基于内容ID列表加载内容标题和摘要等简要信息，供feed item展示使用，减少feed item加载时的数据库查询压力
func (r *Repository) loadBriefMap(ctx context.Context, ids []int64) (map[int64]feedBrief, error) {
	briefMap := make(map[int64]feedBrief, len(ids))
	if r.db == nil {
		return briefMap, nil
	}

	var articles []model.Article
	if err := r.db.WithContext(ctx).Where("content_id IN ? AND is_deleted = ?", ids, false).Find(&articles).Error; err != nil {
		return nil, err
	}
	for _, article := range articles {
		summary := article.Description
		if summary == "" {
			summary = article.Content
		}
		if len(summary) > 96 {
			summary = summary[:96]
		}
		briefMap[article.ContentID] = feedBrief{
			Title:   article.Title,
			Summary: summary,
		}
	}

	var videos []model.Video
	if err := r.db.WithContext(ctx).Where("content_id IN ? AND is_deleted = ?", ids, false).Find(&videos).Error; err != nil {
		return nil, err
	}
	for _, video := range videos {
		briefMap[video.ContentID] = feedBrief{
			Title:   video.Title,
			Summary: fmt.Sprintf("video duration %ds", video.Duration),
		}
	}

	return briefMap, nil
}
