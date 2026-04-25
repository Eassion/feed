package feedsvc

import (
	"context"
	"errors"
	"time"

	"github.com/zeromicro/go-zero/core/mr"

	"feed/internal/cache"
	"feed/internal/model"
	contentrepo "feed/internal/repository/content"
	feedrepo "feed/internal/repository/feed"
	countsvc "feed/internal/service/count"
	interactionsvc "feed/internal/service/interaction"
	usersvc "feed/internal/service/user"
	"feed/pkg/gosafe"
)

var ErrInvalidFeedRequest = errors.New("invalid feed request")

type Service struct {
	repo         *feedrepo.Repository
	users        *usersvc.Service
	interactions *interactionsvc.Service
	counts       *countsvc.Service
	contentRepo  *contentrepo.Repository
	hotrank      *cache.HotRankStore
}

const publishSeedScore = 2.4
const hotRankLikeDelta = 3
const hotRankFavoriteDelta = 5
const hotRankCommentDelta = 4

type FollowBackfillHandler interface {
	HandleUserFollowed(ctx context.Context, followerID, followeeID int64)
}

type FavoriteListHandler interface {
	HandleUserFavoriteChanged(ctx context.Context, userID, contentID, favoriteID int64, active bool)
}

func New(repo *feedrepo.Repository, users *usersvc.Service, interactions *interactionsvc.Service, counts *countsvc.Service, contentRepo *contentrepo.Repository, hotrank *cache.HotRankStore) *Service {
	return &Service{
		repo:         repo,
		users:        users,
		interactions: interactions,
		counts:       counts,
		contentRepo:  contentRepo,
		hotrank:      hotrank,
	}
}

type RecommendRequest struct {
	UserID     int64
	Limit      int
	Cursor     string
	SnapshotID string
}

type FollowRequest struct {
	UserID int64
	Limit  int
	Cursor string
}

type UserPublishRequest struct {
	UserID   int64
	AuthorID int64
	Limit    int
	Cursor   string
}

type UserFavoriteRequest struct {
	ViewerID       int64
	FavoriteUserID int64
	Limit          int
	Cursor         string
}

func (s *Service) ListRecommended(ctx context.Context, req RecommendRequest) (*feedrepo.FeedPage, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	//从热榜缓存中读：用户指定snapshotID > 最新snapshotID > global snapshotID
	page, err := s.repo.ListRecommended(ctx, feedrepo.RecommendQuery{
		SnapshotID: req.SnapshotID,
		Cursor:     req.Cursor,
		Limit:      req.Limit,
	})
	if err != nil {
		return nil, err
	}

	//聚合feed其他信息
	if err := s.enrichFeedItems(ctx, req.UserID, page.Items); err != nil {
		return nil, err
	}

	return page, nil
}

func (s *Service) ListFollowing(ctx context.Context, req FollowRequest) (*feedrepo.FeedPage, error) {
	if req.UserID <= 0 {
		return nil, ErrInvalidFeedRequest
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	//确保关注流inbox存在，第一次访问时会进行冷启动
	exists, err := s.repo.InboxExists(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		//冷启动  重建inbox
		if err := s.coldStartInbox(ctx, req.UserID); err != nil {
			return nil, err
		}
	}

	//从关注流inbox中读 也就是从缓存读，然后从数据库聚合title和summary信息
	page, err := s.repo.ListFollowing(ctx, feedrepo.FollowQuery{
		UserID: req.UserID,
		Cursor: req.Cursor,
		Limit:  req.Limit,
	})
	if err != nil {
		return nil, err
	}

	//聚合作者信息  当前用户点赞状态  内容的统计数据
	if err := s.enrichFeedItems(ctx, req.UserID, page.Items); err != nil {
		return nil, err
	}

	return page, nil
}

func (s *Service) ListUserPublished(ctx context.Context, req UserPublishRequest) (*feedrepo.FeedPage, error) {
	if req.AuthorID <= 0 {
		return nil, ErrInvalidFeedRequest
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	exists, err := s.repo.UserPublishExists(ctx, req.AuthorID)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := s.rebuildUserPublish(ctx, req.AuthorID, true); err != nil {
			return nil, err
		}
	}

	page, err := s.repo.ListUserPublished(ctx, feedrepo.UserPublishQuery{
		AuthorID: req.AuthorID,
		Cursor:   req.Cursor,
		Limit:    req.Limit,
	})
	if err != nil {
		return nil, err
	}

	if err := s.enrichFeedItems(ctx, req.UserID, page.Items); err != nil {
		return nil, err
	}

	return page, nil
}

func (s *Service) ListUserFavorited(ctx context.Context, req UserFavoriteRequest) (*feedrepo.FeedPage, error) {
	if req.FavoriteUserID <= 0 {
		return nil, ErrInvalidFeedRequest
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	exists, err := s.repo.UserFavoriteExists(ctx, req.FavoriteUserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := s.rebuildUserFavorite(ctx, req.FavoriteUserID, true); err != nil {
			return nil, err
		}
	}

	page, err := s.repo.ListUserFavorited(ctx, feedrepo.UserFavoriteQuery{
		UserID: req.FavoriteUserID,
		Cursor: req.Cursor,
		Limit:  req.Limit,
	})
	if err != nil {
		return nil, err
	}

	if err := s.enrichFeedItems(ctx, req.ViewerID, page.Items); err != nil {
		return nil, err
	}

	return page, nil
}

func (s *Service) HandleContentPublished(ctx context.Context, authorID, contentID int64, visibility string, createdAt time.Time) {
	if authorID <= 0 || contentID <= 0 || visibility != model.ContentVisibilityPublic {
		return
	}

	// 写入作者发布流
	if s.repo != nil {
		_ = s.repo.AddToUserPublish(ctx, authorID, contentID, createdAt)
	}
	//给热榜增加初始分
	if s.hotrank != nil {
		_ = s.hotrank.AddDelta(ctx, contentID, publishSeedScore)
	}

	//推送到粉丝关注流inbox
	s.FanoutNewContent(ctx, authorID, contentID, createdAt)
}

func (s *Service) HandleContentDeleted(ctx context.Context, authorID, contentID int64) {
	if authorID <= 0 || contentID <= 0 {
		return
	}

	// 从作者发布流删除
	if s.repo != nil {
		_ = s.repo.RemoveFromUserPublish(ctx, authorID, contentID)
	}
	// 从热榜中删除
	if s.hotrank != nil {
		_ = s.hotrank.RemoveContent(ctx, contentID)
	}
}

func (s *Service) HandleUserLikeChanged(ctx context.Context, contentID, authorID, delta int64) {
	if contentID <= 0 || authorID <= 0 || delta == 0 {
		return
	}

	if s.counts != nil {
		//更新点赞计数
		_ = s.counts.AddLike(ctx, contentID, authorID, delta)
		//删除计数缓存
		_ = s.counts.InvalidateContentCounter(ctx, contentID)
		//删除与用户相关的计数缓存
		_ = s.counts.InvalidateUserCounter(ctx, authorID)
	}
	if s.hotrank != nil {
		//更新热榜
		_ = s.hotrank.AddDelta(ctx, contentID, float64(hotRankLikeDelta*delta))
	}
}

func (s *Service) HandleCommentChanged(ctx context.Context, comment *model.Comment, delta int64) {
	if comment == nil || comment.ContentID <= 0 || delta == 0 {
		return
	}

	if s.counts != nil {
		//更新评论计数
		_ = s.counts.AddComment(ctx, comment.ContentID, delta)
		//删除旧的内容计数缓存
		_ = s.counts.InvalidateContentCounter(ctx, comment.ContentID)
	}
	if s.hotrank != nil {
		//更新热榜
		_ = s.hotrank.AddDelta(ctx, comment.ContentID, float64(hotRankCommentDelta*delta))
	}
}

func (s *Service) HandleUserFavoriteChanged(ctx context.Context, userID, contentID, favoriteID int64, active bool) {
	if userID <= 0 || contentID <= 0 {
		return
	}

	delta := int64(1)
	if !active {
		delta = -1
	}
	if s.counts != nil && s.contentRepo != nil {
		if authorID, err := s.contentRepo.GetAuthorID(ctx, contentID); err == nil && authorID > 0 {
			_ = s.counts.AddFavorite(ctx, contentID, authorID, delta)
			_ = s.counts.InvalidateContentCounter(ctx, contentID)
			_ = s.counts.InvalidateUserCounter(ctx, authorID)
		}
	}
	if s.hotrank != nil {
		_ = s.hotrank.AddDelta(ctx, contentID, float64(hotRankFavoriteDelta*delta))
	}

	if s.repo != nil {
		exists, err := s.repo.UserFavoriteExists(ctx, userID)
		if err != nil || !exists {
			return
		}

		if active {
			if favoriteID <= 0 {
				_ = s.repo.InvalidateUserFavorite(ctx, userID)
				return
			}
			_ = s.repo.AddToUserFavorite(ctx, userID, contentID, favoriteID)
			return
		}

		_ = s.repo.RemoveFromUserFavorite(ctx, userID, contentID)
	}
}

//
func (s *Service) HandleUserFollowed(ctx context.Context, followerID, followeeID int64) {
	if s.repo == nil || followerID <= 0 || followeeID <= 0 || followerID == followeeID {
		return
	}

	gosafe.Go(nil, func() {
		//从被关注者缓存中读取最近发布的limit条内容
		entries, err := s.repo.ListUserPublishEntries(context.Background(), followeeID, cache.FollowInboxRebuildLimit)
		if err != nil {
			return
		}

		//缓存中没有的话就从数据库读
		if len(entries) == 0 && s.contentRepo != nil {
			contents, err := s.contentRepo.ListRecentByAuthors(context.Background(), []int64{followeeID}, cache.FollowInboxRebuildLimit)
			if err != nil {
				return
			}
			entries = make([]cache.InboxEntry, 0, len(contents))
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
		}

		if len(entries) == 0 || s.repo == nil {
			return
		}
		//推送到粉丝关注流inbox  也就是写入缓存
		_ = s.repo.AddToInboxEntries(context.Background(), followerID, entries)
	})
}

func (s *Service) FanoutNewContent(ctx context.Context, authorID, contentID int64, createdAt time.Time) {
	if authorID <= 0 || contentID <= 0 {
		return
	}

	gosafe.Go(nil, func() {
		followerIDs, err := s.interactions.ListFollowerIDs(context.Background(), authorID)
		if err != nil || len(followerIDs) == 0 {
			return
		}
		_ = s.repo.PushToFollowersInbox(context.Background(), followerIDs, contentID, createdAt)
	})
}

func (s *Service) coldStartInbox(ctx context.Context, userID int64) error {
	return s.rebuildInbox(ctx, userID, true)
}

func (s *Service) RebuildInbox(ctx context.Context, userID int64) error {
	return s.rebuildInbox(ctx, userID, true)
}

func (s *Service) BackfillAllFollowInboxes(ctx context.Context, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 200
	}

	var afterUserID int64
	for {
		userIDs, err := s.interactions.ListFollowerUserIDsForRebuild(ctx, afterUserID, batchSize)
		if err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return nil
		}

		for _, userID := range userIDs {
			if err := s.rebuildInbox(ctx, userID, true); err != nil {
				return err
			}
			afterUserID = userID
		}

		if len(userIDs) < batchSize {
			return nil
		}
	}
}

func (s *Service) RebuildUserPublish(ctx context.Context, authorID int64) error {
	return s.rebuildUserPublish(ctx, authorID, true)
}

func (s *Service) RebuildUserFavorite(ctx context.Context, userID int64) error {
	return s.rebuildUserFavorite(ctx, userID, true)
}

func (s *Service) rebuildInbox(ctx context.Context, userID int64, acquireLock bool) error {
	if acquireLock {
		//冷启动和重建都需要获取锁，避免多个协程同时重建同一个用户的关注流
		//使用的是SetNX+TTL的方式，锁的有效期是10分钟，重建完成后会自动过期释放锁，如果重建过程中发生错误没有正常释放锁，10分钟后也能自动释放，避免死锁
		acquired, err := s.repo.TryAcquireInboxRebuildLock(ctx, userID)
		if err != nil {
			return err
		}
		if !acquired {
			return nil
		}
	}

	//拿到了锁
	//查看关注了谁
	followeeIDs, err := s.interactions.ListFolloweeIDs(ctx, userID)
	if err != nil {
		return err
	}

	contents := []model.Content{}
	if len(followeeIDs) > 0 {
		contents, err = s.contentRepo.ListRecentByAuthors(ctx, followeeIDs, cache.FollowInboxRebuildLimit)
		if err != nil {
			return err
		}
	}

	return s.repo.RebuildInbox(ctx, userID, contents)
}

func (s *Service) rebuildUserPublish(ctx context.Context, authorID int64, acquireLock bool) error {
	if s.repo == nil || s.contentRepo == nil || authorID <= 0 {
		return nil
	}

	if acquireLock {
		acquired, err := s.repo.TryAcquireUserPublishRebuildLock(ctx, authorID)
		if err != nil {
			return err
		}
		if !acquired {
			return nil
		}
	}

	contents, err := s.contentRepo.ListRecentByAuthors(ctx, []int64{authorID}, cache.UserPublishRebuildLimit)
	if err != nil {
		return err
	}

	return s.repo.RebuildUserPublish(ctx, authorID, contents)
}

func (s *Service) rebuildUserFavorite(ctx context.Context, userID int64, acquireLock bool) error {
	if s.repo == nil || s.interactions == nil || userID <= 0 {
		return nil
	}

	if acquireLock {
		acquired, err := s.repo.TryAcquireUserFavoriteRebuildLock(ctx, userID)
		if err != nil {
			return err
		}
		if !acquired {
			return nil
		}
	}

	favorites, err := s.interactions.ListFavoriteEntries(ctx, userID, cache.UserFavoriteRebuildLimit)
	if err != nil {
		return err
	}

	return s.repo.RebuildUserFavorite(ctx, userID, favorites)
}


//聚合feed其他信息
func (s *Service) enrichFeedItems(ctx context.Context, userID int64, items []model.FeedItem) error {
	if len(items) == 0 {
		return nil
	}

	authorIDs := make([]int64, 0, len(items))
	contentIDs := make([]int64, 0, len(items))
	//去重作者ID，避免重复查询用户信息
	seenAuthors := make(map[int64]struct{})
	for _, item := range items {
		contentIDs = append(contentIDs, item.ID)
		if _, ok := seenAuthors[item.AuthorID]; !ok {
			seenAuthors[item.AuthorID] = struct{}{}
			authorIDs = append(authorIDs, item.AuthorID)
		}
	}

	var (
		userMap  map[int64]model.UserSummary
		likeMap  map[int64]bool
		countMap map[int64]model.ContentCount
	)

	//并行查询用户信息、当前用户对内容的点赞状态、内容的统计数据
	if err := mr.Finish(
		func() error {
			var err error
			userMap, err = s.users.BatchGetUserMap(ctx, authorIDs)
			return err
		},
		func() error {
			var err error
			likeMap, err = s.interactions.BatchQueryLikeInfoMap(ctx, userID, contentIDs)
			return err
		},
		func() error {
			var err error
			//尝试从缓存读取，miss的contentID会被传到下一步查询数据库
			//miss从数据库查到以后会回填缓存
			countMap, err = s.counts.BatchGetContentCountMap(ctx, contentIDs)
			return err
		},
	); err != nil {
		return err
	}

	for idx := range items {
		//根据authorID获取作者信息
		if author, ok := userMap[items[idx].AuthorID]; ok {
			items[idx].Author = &author
		}
		//写入当前用户对内容的点赞状态
		items[idx].IsLiked = likeMap[items[idx].ID]
		//写入内容的统计数据
		if counter, ok := countMap[items[idx].ID]; ok {
			items[idx].LikeCount = counter.LikeCount
			items[idx].FavoriteCount = counter.FavoriteCount
			items[idx].CommentCount = counter.CommentCount
		}
	}

	return nil
}
