package contentrepo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"feed/internal/model"
	"gorm.io/gorm"
)

var (
	ErrRepositoryUnavailable = errors.New("content repository is unavailable")
	ErrContentNotFound       = errors.New("content not found")
	ErrContentForbidden      = errors.New("content access forbidden")
)

type Repository struct {
	db *gorm.DB
}

type PublishListPage struct {
	Items      []model.ContentListItem
	NextCursor string
	HasMore    bool
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateArticle(ctx context.Context, content *model.Content, article *model.Article) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}

	now := time.Now()
	if content.PublishedAt.IsZero() {
		content.PublishedAt = now
	}
	if content.Status == 0 {
		content.Status = model.ContentStatusPublished
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(content).Error; err != nil {
			return err
		}
		article.ContentID = content.ID
		return tx.Create(article).Error
	})
}

func (r *Repository) CreateVideo(ctx context.Context, content *model.Content, video *model.Video) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}

	now := time.Now()
	if content.PublishedAt.IsZero() {
		content.PublishedAt = now
	}
	if content.Status == 0 {
		content.Status = model.ContentStatusPublished
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(content).Error; err != nil {
			return err
		}
		video.ContentID = content.ID
		return tx.Create(video).Error
	})
}

func (r *Repository) MarkVideoTranscoding(ctx context.Context, contentID int64) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if contentID <= 0 {
		return ErrContentNotFound
	}

	return r.db.WithContext(ctx).
		Model(&model.Video{}).
		Where("content_id = ? AND is_deleted = ?", contentID, false).
		Updates(map[string]any{
			"transcode_status": model.VideoTranscodeStatusProcessing,
			"fail_reason":      "",
			"updated_at":       time.Now(),
		}).Error
}

func (r *Repository) MarkVideoTranscodeSucceeded(ctx context.Context, contentID int64, hlsURL, mediaID string) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if contentID <= 0 {
		return ErrContentNotFound
	}

	return r.db.WithContext(ctx).
		Model(&model.Video{}).
		Where("content_id = ? AND is_deleted = ?", contentID, false).
		Updates(map[string]any{
			"hls_url":          hlsURL,
			"media_id":         mediaID,
			"transcode_status": model.VideoTranscodeStatusSucceeded,
			"fail_reason":      "",
			"updated_at":       time.Now(),
		}).Error
}

func (r *Repository) MarkVideoTranscodeFailed(ctx context.Context, contentID int64, reason string) error {
	if r.db == nil {
		return ErrRepositoryUnavailable
	}
	if contentID <= 0 {
		return ErrContentNotFound
	}

	return r.db.WithContext(ctx).
		Model(&model.Video{}).
		Where("content_id = ? AND is_deleted = ?", contentID, false).
		Updates(map[string]any{
			"transcode_status": model.VideoTranscodeStatusFailed,
			"fail_reason":      trimFailReason(reason),
			"updated_at":       time.Now(),
		}).Error
}

//从content表获取基础信息，再去article/video表获取详情信息，组装成ContentDetail返回
func (r *Repository) GetDetail(ctx context.Context, contentID, viewerID int64) (*model.ContentDetail, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	content := &model.Content{}
	query := r.db.WithContext(ctx).
		Where("id = ? AND status = ? AND is_deleted = ?", contentID, model.ContentStatusPublished, false)
	if viewerID > 0 {
		//如果viewerID存在，说明是登录用户，可以看到公开内容和自己的私密内容；如果viewerID不存在，说明是未登录用户，只能看到公开内容
		query = query.Where("(visibility = ? OR user_id = ?)", model.ContentVisibilityPublic, viewerID)
	} else {
		query = query.Where("visibility = ?", model.ContentVisibilityPublic)
	}
	if err := query.First(content).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrContentNotFound
		}
		return nil, err
	}

	//注意这里的三个count会被后面专门查count_value表的GetContentCounter覆盖，所以不需要担心一致性问题
	detail := &model.ContentDetail{
		ID:            content.ID,
		AuthorID:      content.UserID,
		Type:          model.ContentTypeName(content.ContentType),
		Status:        content.Status,
		Visibility:    content.Visibility,
		LikeCount:     content.LikeCount,
		FavoriteCount: content.FavoriteCount,
		CommentCount:  content.CommentCount,
		HotScore:      content.HotScore,
		CreatedAt:     content.CreatedAt,
		PublishedAt:   content.PublishedAt,
		UpdatedAt:     content.UpdatedAt,
	}

	switch content.ContentType {
	case model.ContentTypeArticle:
		article := &model.Article{}
		if err := r.db.WithContext(ctx).Where("content_id = ? AND is_deleted = ?", content.ID, false).First(article).Error; err != nil {
			return nil, err
		}
		detail.Title = article.Title
		detail.Description = article.Description
		detail.CoverURL = article.Cover
		detail.Text = article.Content
	case model.ContentTypeVideo:
		video := &model.Video{}
		if err := r.db.WithContext(ctx).Where("content_id = ? AND is_deleted = ?", content.ID, false).First(video).Error; err != nil {
			return nil, err
		}
		detail.Title = video.Title
		detail.CoverURL = video.CoverURL
		detail.VideoURL = video.OriginURL
		detail.HLSURL = video.HLSURL
		detail.MediaID = video.MediaID
		detail.Duration = video.Duration
	default:
		return nil, fmt.Errorf("unsupported content type %d", content.ContentType)
	}

	return detail, nil
}

func (r *Repository) DeleteByAuthor(ctx context.Context, contentID, authorID int64) (*model.Content, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}

	content := &model.Content{}
	if err := r.db.WithContext(ctx).Where("id = ? AND is_deleted = ?", contentID, false).First(content).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrContentNotFound
		}
		return nil, err
	}
	if content.UserID != authorID {
		return nil, ErrContentForbidden
	}

	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		switch content.ContentType {
		case model.ContentTypeArticle:
			if err := tx.Model(&model.Article{}).
				Where("content_id = ? AND is_deleted = ?", contentID, false).
				Updates(map[string]any{
					"is_deleted": true,
					"updated_at": now,
				}).Error; err != nil {
				return err
			}
		case model.ContentTypeVideo:
			if err := tx.Model(&model.Video{}).
				Where("content_id = ? AND is_deleted = ?", contentID, false).
				Updates(map[string]any{
					"is_deleted": true,
					"updated_at": now,
				}).Error; err != nil {
				return err
			}
		}

		return tx.Model(&model.Content{}).
			Where("id = ? AND user_id = ? AND is_deleted = ?", contentID, authorID, false).
			Updates(map[string]any{
				"status":     model.ContentStatusDeleted,
				"is_deleted": true,
				"updated_by": authorID,
				"updated_at": now,
			}).Error
	})
	if err != nil {
		return nil, err
	}

	content.Status = model.ContentStatusDeleted
	content.IsDeleted = true
	content.UpdatedBy = authorID
	content.UpdatedAt = now
	return content, nil
}

func (r *Repository) ListByAuthor(ctx context.Context, authorID int64, cursor string, limit int) (*PublishListPage, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	if limit <= 0 {
		limit = 20
	}

	query := r.db.WithContext(ctx).
		Model(&model.Content{}).
		Where("user_id = ? AND status = ? AND is_deleted = ? AND visibility = ?", authorID, model.ContentStatusPublished, false, model.ContentVisibilityPublic)

	if ts, id, ok := parsePublishCursor(cursor); ok {
		query = query.Where("published_at < ? OR (published_at = ? AND id < ?)", ts, ts, id)
	}

	var contents []model.Content
	if err := query.Order("published_at DESC, id DESC").Limit(limit + 1).Find(&contents).Error; err != nil {
		return nil, err
	}

	hasMore := len(contents) > limit
	if hasMore {
		contents = contents[:limit]
	}

	items, err := r.buildPublishListItems(ctx, contents)
	if err != nil {
		return nil, err
	}

	return &PublishListPage{
		Items:      items,
		NextCursor: buildPublishCursor(items),
		HasMore:    hasMore,
	}, nil
}

func (r *Repository) CountPublicPublishedByAuthor(ctx context.Context, authorID int64) (int64, error) {
	if r.db == nil {
		return 0, ErrRepositoryUnavailable
	}
	if authorID <= 0 {
		return 0, nil
	}

	var count int64
	if err := r.db.WithContext(ctx).
		Model(&model.Content{}).
		Where("user_id = ? AND status = ? AND is_deleted = ? AND visibility = ?", authorID, model.ContentStatusPublished, false, model.ContentVisibilityPublic).
		Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

func (r *Repository) GetAuthorID(ctx context.Context, contentID int64) (int64, error) {
	if r.db == nil {
		return 0, ErrRepositoryUnavailable
	}

	content := &model.Content{}
	if err := r.db.WithContext(ctx).
		Select("id", "user_id").
		Where("id = ? AND is_deleted = ?", contentID, false).
		First(content).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrContentNotFound
		}
		return 0, err
	}

	return content.UserID, nil
}

//按发布时间倒序取出authorID列表发布公开未被删除的limit条内容
func (r *Repository) ListRecentByAuthors(ctx context.Context, authorIDs []int64, limit int64) ([]model.Content, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	if len(authorIDs) == 0 || limit <= 0 {
		return []model.Content{}, nil
	}

	var contents []model.Content
	if err := r.db.WithContext(ctx).
		Where("user_id IN ? AND status = ? AND is_deleted = ? AND visibility = ?", authorIDs, model.ContentStatusPublished, false, model.ContentVisibilityPublic).
		Order("published_at DESC, id DESC").
		Limit(int(limit)).
		Find(&contents).Error; err != nil {
		return nil, err
	}

	return contents, nil
}

func (r *Repository) SyncHotScores(ctx context.Context, scores map[int64]float64, at time.Time) error {
	if r.db == nil || len(scores) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for contentID, score := range scores {
			if err := tx.Model(&model.Content{}).
				Where("id = ? AND is_deleted = ?", contentID, false).
				Updates(map[string]any{
					"hot_score":         score,
					"last_hot_score_at": at,
					"updated_at":        at,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) ListHotScores(ctx context.Context, limit int64) (map[int64]float64, error) {
	if r.db == nil {
		return nil, ErrRepositoryUnavailable
	}
	if limit <= 0 {
		limit = 5000
	}

	var contents []model.Content
	if err := r.db.WithContext(ctx).
		Select("id", "hot_score").
		Where("status = ? AND is_deleted = ? AND visibility = ? AND hot_score > ?", model.ContentStatusPublished, false, model.ContentVisibilityPublic, 0).
		Order("hot_score DESC, published_at DESC, id DESC").
		Limit(int(limit)).
		Find(&contents).Error; err != nil {
		return nil, err
	}

	scores := make(map[int64]float64, len(contents))
	for _, content := range contents {
		scores[content.ID] = content.HotScore
	}
	return scores, nil
}

func (r *Repository) buildPublishListItems(ctx context.Context, contents []model.Content) ([]model.ContentListItem, error) {
	if len(contents) == 0 {
		return []model.ContentListItem{}, nil
	}

	articleIDs := make([]int64, 0)
	videoIDs := make([]int64, 0)
	for _, item := range contents {
		switch item.ContentType {
		case model.ContentTypeArticle:
			articleIDs = append(articleIDs, item.ID)
		case model.ContentTypeVideo:
			videoIDs = append(videoIDs, item.ID)
		}
	}

	articleMap := make(map[int64]model.Article)
	if len(articleIDs) > 0 {
		var articles []model.Article
		if err := r.db.WithContext(ctx).Where("content_id IN ? AND is_deleted = ?", articleIDs, false).Find(&articles).Error; err != nil {
			return nil, err
		}
		for _, article := range articles {
			articleMap[article.ContentID] = article
		}
	}

	videoMap := make(map[int64]model.Video)
	if len(videoIDs) > 0 {
		var videos []model.Video
		if err := r.db.WithContext(ctx).Where("content_id IN ? AND is_deleted = ?", videoIDs, false).Find(&videos).Error; err != nil {
			return nil, err
		}
		for _, video := range videos {
			videoMap[video.ContentID] = video
		}
	}

	items := make([]model.ContentListItem, 0, len(contents))
	for _, item := range contents {
		listItem := model.ContentListItem{
			ID:          item.ID,
			AuthorID:    item.UserID,
			Type:        model.ContentTypeName(item.ContentType),
			CreatedAt:   item.CreatedAt,
			PublishedAt: item.PublishedAt,
		}

		if article, ok := articleMap[item.ID]; ok {
			listItem.Title = article.Title
			listItem.Description = article.Description
			listItem.CoverURL = article.Cover
		}
		if video, ok := videoMap[item.ID]; ok {
			listItem.Title = video.Title
			listItem.CoverURL = video.CoverURL
		}

		items = append(items, listItem)
	}

	return items, nil
}

func parsePublishCursor(cursor string) (time.Time, int64, bool) {
	if cursor == "" {
		return time.Time{}, 0, false
	}

	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return time.Time{}, 0, false
	}

	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0, false
	}

	return time.UnixMilli(millis), id, true
}

func buildPublishCursor(items []model.ContentListItem) string {
	if len(items) == 0 {
		return ""
	}

	last := items[len(items)-1]
	return fmt.Sprintf("%d:%d", last.PublishedAt.UnixMilli(), last.ID)
}

func trimFailReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 {
		return reason[:1000]
	}
	return reason
}
