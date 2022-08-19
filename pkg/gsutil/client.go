package gsutil

import (
	"cloud.google.com/go/storage"
	"context"
	"google.golang.org/api/option"
)

type Client struct {
	CredentialFile string
	context        context.Context
	client         *storage.Client
}

func NewClient(ctx context.Context, credentials string) (*Client, error) {
	var err error
	out := Client{
		CredentialFile: credentials,
		context:        ctx,
	}
	out.client, err = storage.NewClient(out.context, option.WithCredentialsFile(out.CredentialFile))
	return &out, err
}
