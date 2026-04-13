package feedsvc

import (
	"context"

	feedrepo "feed/internal/repository/feed"
)

type Service struct {
	repo *feedrepo.Repository
}

func New(repo *feedrepo.Repository) *Service {
	return &Service{repo: repo}
}

type RecommendRequest struct {
	UserID     int64
	Limit      int
	Cursor     string
	SnapshotID string
}

func (s *Service) ListRecommended(ctx context.Context, req RecommendRequest) (*feedrepo.RecommendPage, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	_ = req.UserID
	return s.repo.ListRecommended(ctx, feedrepo.RecommendQuery{
		SnapshotID: req.SnapshotID,
		Cursor:     req.Cursor,
		Limit:      req.Limit,
	})
}
