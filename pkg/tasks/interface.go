package tasks

import (
	"FuzzerMan/pkg/config"
	"context"
)

type RunnableTask interface {
	Initialize(ctx context.Context, config *config.Config) error
	Run() error
}
