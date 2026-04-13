package cron

import "context"

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Start(_ context.Context) error {
	return nil
}
