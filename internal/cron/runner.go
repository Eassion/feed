package cron

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"feed/internal/cache"
	"feed/internal/config"
	contentrepo "feed/internal/repository/content"
	feedsvc "feed/internal/service/feed"
	"feed/pkg/gosafe"
)

type Runner struct {
	cfg        config.CronConfig
	logger     *slog.Logger
	hotrank    *cache.HotRankStore
	contents   *contentrepo.Repository
	feeds      *feedsvc.Service
	hotBusy    atomic.Bool
	coldBusy   atomic.Bool
	followBusy atomic.Bool
}

func NewRunner(
	cfg config.CronConfig,
	logger *slog.Logger,
	hotrank *cache.HotRankStore,
	contents *contentrepo.Repository,
	feeds *feedsvc.Service,
) *Runner {
	return &Runner{
		cfg:      cfg,
		logger:   logger,
		hotrank:  hotrank,
		contents: contents,
		feeds:    feeds,
	}
}

func (r *Runner) Start(ctx context.Context) error {
	if r == nil || !r.cfg.Enabled {
		return nil
	}

	r.startTicker(ctx, "hot.fast.update", r.cfg.HotSnapshotInterval, &r.hotBusy, r.runHotSnapshot)
	r.startTicker(ctx, "hot.cold.update", r.cfg.HotColdUpdateInterval, &r.coldBusy, r.runHotColdUpdate)
	r.startTicker(ctx, "follow_inbox_backfill", r.cfg.FollowInboxBackfillInterval, &r.followBusy, r.runFollowInboxBackfill)
	return nil
}

func (r *Runner) startTicker(
	ctx context.Context,
	name string,
	interval time.Duration,
	busy *atomic.Bool,
	job func(context.Context) error,
) {
	if interval <= 0 || job == nil {
		return
	}

	run := func() {
		if busy != nil && !busy.CompareAndSwap(false, true) {
			if r.logger != nil {
				r.logger.Info("skip overlapped cron job", "job", name)
			}
			return
		}
		if busy != nil {
			defer busy.Store(false)
		}

		startedAt := time.Now()
		if err := job(ctx); err != nil {
			if r.logger != nil {
				r.logger.Error("cron job failed", "job", name, "error", err)
			}
			return
		}
		if r.logger != nil {
			r.logger.Info("cron job finished", "job", name, "cost", time.Since(startedAt).String())
		}
	}

	gosafe.Go(r.logger, run)
	gosafe.Go(r.logger, func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	})
}

func (r *Runner) runHotSnapshot(ctx context.Context) error {
	if r.hotrank == nil {
		return nil
	}

	bucket := time.Now().Format("200601021504")
	locked, err := r.hotrank.TryAcquireFastUpdateLock(ctx, bucket, r.cfg.HotSnapshotInterval)
	if err != nil {
		return err
	}
	if !locked {
		if r.logger != nil {
			r.logger.Info("skip hot snapshot because redis lock is held", "bucket", bucket)
		}
		return nil
	}
	defer func() {
		_ = r.hotrank.ReleaseFastUpdateLock(ctx, bucket)
	}()

	if _, err := r.hotrank.MergeDelta(ctx); err != nil {
		return err
	}

	_, scores, err := r.hotrank.BuildSnapshot(ctx, r.cfg.HotSnapshotSize, r.cfg.HotSnapshotTTL)
	if err != nil {
		return err
	}
	if len(scores) == 0 || r.contents == nil {
		return nil
	}

	return r.contents.SyncHotScores(ctx, scores, time.Now())
}

func (r *Runner) runHotColdUpdate(ctx context.Context) error {
	if r.hotrank == nil || r.contents == nil {
		return nil
	}

	date := time.Now().Format("20060102")
	locked, err := r.hotrank.TryAcquireColdUpdateLock(ctx, date, r.cfg.HotColdUpdateInterval)
	if err != nil {
		return err
	}
	if !locked {
		if r.logger != nil {
			r.logger.Info("skip hot cold update because redis lock is held", "date", date)
		}
		return nil
	}

	scores, err := r.contents.ListHotScores(ctx, r.cfg.HotColdUpdateSize)
	if err != nil {
		_ = r.hotrank.ReleaseColdUpdateLock(ctx, date)
		return err
	}
	if err := r.hotrank.ReplaceGlobal(ctx, scores); err != nil {
		_ = r.hotrank.ReleaseColdUpdateLock(ctx, date)
		return err
	}
	if _, _, err := r.hotrank.BuildSnapshot(ctx, r.cfg.HotSnapshotSize, r.cfg.HotSnapshotTTL); err != nil {
		_ = r.hotrank.ReleaseColdUpdateLock(ctx, date)
		return err
	}
	return nil
}

func (r *Runner) runFollowInboxBackfill(ctx context.Context) error {
	if r.feeds == nil {
		return nil
	}
	return r.feeds.BackfillAllFollowInboxes(ctx, r.cfg.FollowInboxBatchSize)
}
