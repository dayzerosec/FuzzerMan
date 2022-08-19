package tasks

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/gsutil"
	"context"
	"errors"
	"path/filepath"
	"strings"
)

type SyncLogTask struct {
	config  *config.Config
	gcs     *gsutil.Client
	context context.Context
	logpath string
}

func (task *SyncLogTask) Initialize(ctx context.Context, config *config.Config) error {
	var err error
	task.config = config
	task.context = ctx

	if config.WorkDirectory == "" {
		return errors.New("missing work directory")
	}

	if config.CloudStorage.CredentialsFile == "" {
		return errors.New("missing credentials file")
	}

	if config.CloudStorage.LogPath == "" {
		return errors.New("missing local path")
	}

	if !strings.HasPrefix(config.CloudStorage.LogPath, "gs://") {
		return errors.New("CloudStorage.LogPath must start with gs://")
	}

	if task.logpath, err = GetWorkDir(config.WorkDirectory, "logs"); err != nil {
		return err
	}

	if task.gcs, err = gsutil.NewClient(ctx, task.config.CloudStorage.CredentialsFile); err != nil {
		return err
	}

	return nil
}

func (task *SyncLogTask) Run() error {
	remote := task.config.CloudStorage.LogPath
	err := task.gcs.Copy(filepath.Join(task.logpath, "*"), remote, false)
	if err != nil {
		return errors.New("failed to copy directory: " + err.Error())
	}
	return nil

}
