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
}

type RecommendQuery struct {
	SnapshotID string
	Cursor     string
	Limit      int
}

type RecommendPage struct {
	Items      []model.FeedItem
	NextCursor string
	HasMore    bool
	SnapshotID string
}

func New(db *gorm.DB, redisClient *redis.Client) *Repository {
	return &Repository{
		db:    db,
		redis: redisClient,
	}
}

func (r *Repository) ListRecommended(ctx context.Context, query RecommendQuery) (*RecommendPage, error) {
	if query.Limit <= 0 {
		query.Limit = 20
	}

	snapshotID := query.SnapshotID
	if snapshotID == "" {
		snapshotID = "global"
	}

	if r.redis != nil {
		ids, scoreMap, nextCursor, hasMore, err := r.listHotIDs(ctx, query)
		if err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			items, err := r.loadFeedItems(ctx, ids, scoreMap)
			if err != nil {
				return nil, err
			}

			return &RecommendPage{
				Items:      items,
				NextCursor: nextCursor,
				HasMore:    hasMore,
				SnapshotID: snapshotID,
			}, nil
		}
	}

	if r.db == nil {
		items := buildFallbackFeed(query.Limit)
		return &RecommendPage{
			Items:      items,
			NextCursor: buildNextCursor(items),
			HasMore:    false,
			SnapshotID: snapshotID,
		}, nil
	}

	var records []feedRecord
	if err := r.db.WithContext(ctx).
		Model(&feedRecord{}).
		Order("created_at DESC").
		Limit(query.Limit + 1).
		Find(&records).Error; err != nil {
		return nil, err
	}

	hasMore := len(records) > query.Limit
	if hasMore {
		records = records[:query.Limit]
	}

	items := make([]model.FeedItem, 0, query.Limit)
	for idx, record := range records {
		items = append(items, model.FeedItem{
			ID:          record.ID,
			AuthorID:    record.AuthorID,
			ContentType: record.Type,
			Title:       fmt.Sprintf("content-%d", record.ID),
			Summary:     "feed item loaded from MySQL",
			Score:       float64(len(records) - idx),
			CreatedAt:   record.CreatedAt,
		})
	}

	if len(items) == 0 {
		items = buildFallbackFeed(query.Limit)
	}

	return &RecommendPage{
		Items:      items,
		NextCursor: buildNextCursor(items),
		HasMore:    hasMore,
		SnapshotID: snapshotID,
	}, nil
}

func (r *Repository) listHotIDs(ctx context.Context, query RecommendQuery) ([]int64, map[int64]float64, string, bool, error) {
	key := cache.FeedHotGlobalKey
	if query.SnapshotID != "" && query.SnapshotID != "global" {
		key = cache.FeedHotSnapshotKey(query.SnapshotID)
	}

	zItems, err := r.redis.ZRevRangeWithScores(ctx, key, 0, 499).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil, "", false, nil
		}
		return nil, nil, "", false, err
	}
	if len(zItems) == 0 {
		return nil, nil, "", false, nil
	}

	cursorScore, cursorID, hasCursor := parseCursor(query.Cursor)
	filtered := make([]redis.Z, 0, query.Limit+1)
	for _, item := range zItems {
		memberID, ok := parseMemberID(item.Member)
		if !ok {
			continue
		}
		if hasCursor {
			if item.Score > cursorScore {
				continue
			}
			if item.Score == cursorScore && memberID >= cursorID {
				continue
			}
		}
		filtered = append(filtered, item)
		if len(filtered) >= query.Limit+1 {
			break
		}
	}

	hasMore := len(filtered) > query.Limit
	if hasMore {
		filtered = filtered[:query.Limit]
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

func (r *Repository) loadFeedItems(ctx context.Context, ids []int64, scoreMap map[int64]float64) ([]model.FeedItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	if r.db == nil {
		items := buildFallbackFeed(len(ids))
		for i := range items {
			if score, ok := scoreMap[items[i].ID]; ok {
				items[i].Score = score
			}
		}
		return items, nil
	}

	var records []feedRecord
	if err := r.db.WithContext(ctx).
		Model(&feedRecord{}).
		Where("id IN ?", ids).
		Find(&records).Error; err != nil {
		return nil, err
	}

	recordMap := make(map[int64]feedRecord, len(records))
	for _, record := range records {
		recordMap[record.ID] = record
	}

	items := make([]model.FeedItem, 0, len(ids))
	for _, id := range ids {
		record, ok := recordMap[id]
		if !ok {
			continue
		}
		items = append(items, model.FeedItem{
			ID:          record.ID,
			AuthorID:    record.AuthorID,
			ContentType: record.Type,
			Title:       fmt.Sprintf("content-%d", record.ID),
			Summary:     "feed item loaded from MySQL",
			Score:       scoreMap[id],
			CreatedAt:   record.CreatedAt,
		})
	}

	return items, nil
}

func buildFallbackFeed(limit int) []model.FeedItem {
	items := make([]model.FeedItem, 0, limit)
	now := time.Now()
	for i := 0; i < limit; i++ {
		items = append(items, model.FeedItem{
			ID:          int64(i + 1),
			AuthorID:    1000 + int64(i),
			ContentType: "article",
			Title:       fmt.Sprintf("demo-feed-item-%d", i+1),
			Summary:     "replace this fallback with real feed aggregation logic",
			Score:       float64(limit - i),
			CreatedAt:   now.Add(-time.Duration(i) * time.Minute),
		})
	}

	return items
}

type feedRecord struct {
	ID        int64     `gorm:"column:id"`
	AuthorID  int64     `gorm:"column:author_id"`
	Type      string    `gorm:"column:type"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (feedRecord) TableName() string {
	return "content"
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
