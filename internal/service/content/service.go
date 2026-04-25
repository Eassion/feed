package contentsvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"feed/internal/model"
	contentrepo "feed/internal/repository/content"
	"feed/pkg/gosafe"
	"github.com/zeromicro/go-zero/core/mr"
)

var (
	ErrInvalidContent    = errors.New("invalid content payload")
	ErrUnsupportedType   = errors.New("unsupported content type")
	ErrContentNotFound   = contentrepo.ErrContentNotFound
	ErrContentForbidden  = contentrepo.ErrContentForbidden
	ErrRepositoryMissing = contentrepo.ErrRepositoryUnavailable
)

type Service struct {
	repo         *contentrepo.Repository
	publisher    ContentPublisher
	commentCache ContentCommentCleaner
	transcoder   VideoTranscoder
	users        ContentUserProvider
	interactions ContentInteractionProvider
	counts       ContentCountProvider
}

type ContentPublisher interface {
	HandleContentPublished(ctx context.Context, authorID, contentID int64, visibility string, createdAt time.Time)
	HandleContentDeleted(ctx context.Context, authorID, contentID int64)
}

type ContentUserProvider interface {
	BatchGetUserMap(ctx context.Context, userIDs []int64) (map[int64]model.UserSummary, error)
}

//写成接口的形式，方便后续增加更多的交互类型（比如收藏、关注等），也方便单测时mock
type ContentInteractionProvider interface {
	BatchQueryLikeInfoMap(ctx context.Context, userID int64, contentIDs []int64) (map[int64]bool, error)
	BatchQueryFavoriteInfoMap(ctx context.Context, userID int64, contentIDs []int64) (map[int64]bool, error)
	IsFollowing(ctx context.Context, followerID, followeeID int64) (bool, error)
}

type ContentCountProvider interface {
	GetContentCounter(ctx context.Context, contentID int64) (*model.ContentCount, error)
}

type ContentCommentCleaner interface {
	HandleContentDeleted(ctx context.Context, contentID int64)
}

type VideoTranscoder interface {
	TranscodeVideo(ctx context.Context, originURL string) (hlsURL string, mediaID string, err error)
}

type PublishArticleRequest struct {
	AuthorID       int64
	Title          string
	CoverURL       string
	ArticleContent string
	Visibility     string
}

type PublishVideoRequest struct {
	AuthorID   int64
	Title      string
	CoverURL   string
	VideoURL   string
	Duration   int64
	Visibility string
}

func New(repo *contentrepo.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) SetPublisher(publisher ContentPublisher) {
	s.publisher = publisher
}

func (s *Service) SetCommentCleaner(cleaner ContentCommentCleaner) {
	s.commentCache = cleaner
}

func (s *Service) SetVideoTranscoder(transcoder VideoTranscoder) {
	s.transcoder = transcoder
}

func (s *Service) SetDetailProviders(users ContentUserProvider, interactions ContentInteractionProvider, counts ContentCountProvider) {
	s.users = users
	s.interactions = interactions
	s.counts = counts
}

func (s *Service) PublishArticle(ctx context.Context, req PublishArticleRequest) (*model.ContentDetail, error) {
	if req.AuthorID <= 0 || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.ArticleContent) == "" {
		return nil, ErrInvalidContent
	}
	visibility, ok := normalizeVisibility(req.Visibility)
	if !ok {
		return nil, ErrInvalidContent
	}

	content := &model.Content{
		UserID:      req.AuthorID,
		ContentType: model.ContentTypeArticle,
		Status:      model.ContentStatusPublished,
		Visibility:  visibility,
		Version:     1,
		CreatedBy:   req.AuthorID,
		UpdatedBy:   req.AuthorID,
	}
	article := &model.Article{
		Title:       strings.TrimSpace(req.Title),
		Cover:       strings.TrimSpace(req.CoverURL),
		Content:     strings.TrimSpace(req.ArticleContent),
		Description: trimSummary(req.ArticleContent),
		Version:     1,
	}

	if err := s.repo.CreateArticle(ctx, content, article); err != nil {
		return nil, err
	}

	if s.publisher != nil {
		s.publisher.HandleContentPublished(ctx, content.UserID, content.ID, content.Visibility, content.PublishedAt)
	}

	return buildArticleDetail(content, article), nil
}

func (s *Service) PublishVideo(ctx context.Context, req PublishVideoRequest) (*model.ContentDetail, error) {
	if req.AuthorID <= 0 || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.VideoURL) == "" {
		return nil, ErrInvalidContent
	}
	visibility, ok := normalizeVisibility(req.Visibility)
	if !ok {
		return nil, ErrInvalidContent
	}

	content := &model.Content{
		UserID:      req.AuthorID,
		ContentType: model.ContentTypeVideo,
		Status:      model.ContentStatusPublished,
		Visibility:  visibility,
		Version:     1,
		CreatedBy:   req.AuthorID,
		UpdatedBy:   req.AuthorID,
	}
	video := &model.Video{
		Title:           strings.TrimSpace(req.Title),
		OriginURL:       strings.TrimSpace(req.VideoURL),
		CoverURL:        strings.TrimSpace(req.CoverURL),
		Duration:        req.Duration,
		TranscodeStatus: model.VideoTranscodeStatusPending,
		Version:         1,
	}

	//落库
	if err := s.repo.CreateVideo(ctx, content, video); err != nil {
		return nil, err
	}

	s.startVideoTranscode(content.ID, video.OriginURL)

	// 处理feed中的三件事
	if s.publisher != nil {
		s.publisher.HandleContentPublished(ctx, content.UserID, content.ID, content.Visibility, content.PublishedAt)
	}

	//返回详情
	return buildVideoDetail(content, video), nil
}

func (s *Service) startVideoTranscode(contentID int64, originURL string) {
	if s == nil || s.repo == nil || s.transcoder == nil || contentID <= 0 || strings.TrimSpace(originURL) == "" {
		return
	}

	gosafe.Go(nil, func() {
		ctx := context.Background()
		if err := s.repo.MarkVideoTranscoding(ctx, contentID); err != nil {
			return
		}

		hlsURL, mediaID, err := s.transcoder.TranscodeVideo(ctx, originURL)
		if err != nil {
			_ = s.repo.MarkVideoTranscodeFailed(ctx, contentID, err.Error())
			return
		}

		_ = s.repo.MarkVideoTranscodeSucceeded(ctx, contentID, hlsURL, mediaID)
	})
}

func (s *Service) GetDetail(ctx context.Context, contentID, viewerID int64) (*model.ContentDetail, error) {
	if contentID <= 0 {
		return nil, ErrInvalidContent
	}

	// 获取content基础信息
	detail, err := s.repo.GetDetail(ctx, contentID, viewerID)
	if err != nil {
		return nil, err
	}

	// 丰富详情数据
	if err := s.enrichDetail(ctx, viewerID, detail); err != nil {
		return nil, err
	}

	return detail, nil
}

func (s *Service) Delete(ctx context.Context, contentID, authorID int64) error {
	if contentID <= 0 || authorID <= 0 {
		return ErrInvalidContent
	}

	//软删除content
	deletedContent, err := s.repo.DeleteByAuthor(ctx, contentID, authorID)
	if err != nil {
		return err
	}

	// 从feed中删除
	if s.publisher != nil && deletedContent != nil {
		s.publisher.HandleContentDeleted(ctx, deletedContent.UserID, deletedContent.ID)
	}

	// 从评论缓存中删除
	if s.commentCache != nil && deletedContent != nil {
		s.commentCache.HandleContentDeleted(ctx, deletedContent.ID)
	}

	return nil
}

func (s *Service) ListByAuthor(ctx context.Context, authorID int64, cursor string, limit int) (*contentrepo.PublishListPage, error) {
	if authorID <= 0 {
		return nil, ErrInvalidContent
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	return s.repo.ListByAuthor(ctx, authorID, cursor, limit)
}

func normalizeVisibility(visibility string) (string, bool) {
	visibility = strings.TrimSpace(visibility)
	if visibility == "" {
		return model.ContentVisibilityPublic, true
	}
	if visibility != model.ContentVisibilityPublic && visibility != model.ContentVisibilityPrivate {
		return "", false
	}

	return visibility, true
}

func trimSummary(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 128 {
		return text[:128]
	}
	return text
}

func buildArticleDetail(content *model.Content, article *model.Article) *model.ContentDetail {
	if content == nil || article == nil {
		return nil
	}

	return &model.ContentDetail{
		ID:            content.ID,
		AuthorID:      content.UserID,
		Type:          model.ContentTypeName(content.ContentType),
		Status:        content.Status,
		Visibility:    content.Visibility,
		Title:         article.Title,
		Description:   article.Description,
		CoverURL:      article.Cover,
		Text:          article.Content,
		LikeCount:     content.LikeCount,
		FavoriteCount: content.FavoriteCount,
		CommentCount:  content.CommentCount,
		HotScore:      content.HotScore,
		CreatedAt:     content.CreatedAt,
		PublishedAt:   content.PublishedAt,
		UpdatedAt:     content.UpdatedAt,
	}
}

func buildVideoDetail(content *model.Content, video *model.Video) *model.ContentDetail {
	if content == nil || video == nil {
		return nil
	}

	return &model.ContentDetail{
		ID:            content.ID,
		AuthorID:      content.UserID,
		Type:          model.ContentTypeName(content.ContentType),
		Status:        content.Status,
		Visibility:    content.Visibility,
		Title:         video.Title,
		CoverURL:      video.CoverURL,
		VideoURL:      video.OriginURL,
		HLSURL:        video.HLSURL,
		MediaID:       video.MediaID,
		Duration:      video.Duration,
		LikeCount:     content.LikeCount,
		FavoriteCount: content.FavoriteCount,
		CommentCount:  content.CommentCount,
		HotScore:      content.HotScore,
		CreatedAt:     content.CreatedAt,
		PublishedAt:   content.PublishedAt,
		UpdatedAt:     content.UpdatedAt,
	}
}

func (s *Service) enrichDetail(ctx context.Context, viewerID int64, detail *model.ContentDetail) error {
	if detail == nil {
		return nil
	}

	var (
		userMap         map[int64]model.UserSummary
		likeMap         map[int64]bool
		favoriteMap     map[int64]bool
		followingAuthor bool
		counter         *model.ContentCount
	)

	if err := mr.Finish(
		func() error {
			if s.users == nil {
				userMap = map[int64]model.UserSummary{}
				return nil
			}
			var err error
			//批量获取content涉及的用户信息（目前只有作者），并返回一个userID到UserSummary的映射；
			userMap, err = s.users.BatchGetUserMap(ctx, []int64{detail.AuthorID})
			return err
		},
		func() error {
			if s.interactions == nil || viewerID <= 0 {
				likeMap = map[int64]bool{}
				return nil
			}
			var err error
			// 检查viewer是否点赞过该content
			likeMap, err = s.interactions.BatchQueryLikeInfoMap(ctx, viewerID, []int64{detail.ID})
			return err
		},
		func() error {
			if s.interactions == nil || viewerID <= 0 {
				favoriteMap = map[int64]bool{}
				return nil
			}
			var err error
			// 检查viewer是否收藏过该content
			favoriteMap, err = s.interactions.BatchQueryFavoriteInfoMap(ctx, viewerID, []int64{detail.ID})
			return err
		},
		func() error {
			if s.interactions == nil || viewerID <= 0 {
				followingAuthor = false
				return nil
			}
			var err error
			// 检查viewer是否关注了作者
			followingAuthor, err = s.interactions.IsFollowing(ctx, viewerID, detail.AuthorID)
			return err
		},
		func() error {
			if s.counts == nil {
				counter = &model.ContentCount{ContentID: detail.ID}
				return nil
			}
			var err error
			// 获取content的最新计数信息（点赞数、收藏数、评论数）
			counter, err = s.counts.GetContentCounter(ctx, detail.ID)
			return err
		},
	); err != nil {
		return err
	}

	if author, ok := userMap[detail.AuthorID]; ok {
		detail.Author = &author
	}
	detail.IsLiked = likeMap[detail.ID]
	detail.IsFavorited = favoriteMap[detail.ID]
	detail.IsFollowingAuthor = followingAuthor
	if counter != nil {
		detail.LikeCount = counter.LikeCount
		detail.FavoriteCount = counter.FavoriteCount
		detail.CommentCount = counter.CommentCount
	}

	return nil
}
