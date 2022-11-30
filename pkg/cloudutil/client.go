package cloudutil

import (
	"cloud.google.com/go/storage"
	"context"
	"golang.org/x/sync/semaphore"
	"sync"
)

type Client struct {
	CredentialFile string
	context        context.Context
	client         *storage.Client
	bucket         string
	sema           *semaphore.Weighted
	wg             *sync.WaitGroup
}

func NewClient(ctx context.Context, bucketUrl string) *Client {
	out := Client{
		context: ctx,
		bucket:  bucketUrl,
		sema:    semaphore.NewWeighted(16),
		wg:      &sync.WaitGroup{},
	}
	return &out
}
