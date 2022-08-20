package tasks

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/gsutil"
	"context"
	"errors"
	"path/filepath"
	"strings"
)

type SyncArtifactsTask struct {
	config       *config.Config
	gcs          *gsutil.Client
	context      context.Context
	artifactpath string
}

func (task *SyncArtifactsTask) Initialize(ctx context.Context, config *config.Config) error {
	var err error
	task.config = config
	task.context = ctx

	if config.WorkDirectory == "" {
		return errors.New("missing work directory")
	}

	if config.CloudStorage.CredentialsFile == "" {
		return errors.New("missing credentials file")
	}

	if config.CloudStorage.ArtifactPath == "" {
		return errors.New("missing cloud artifact path  path")
	}

	if !strings.HasPrefix(config.CloudStorage.ArtifactPath, "gs://") {
		return errors.New("CloudStorage.ArtifactPath must start with gs://")
	}

	if task.artifactpath, err = GetWorkDir(config.WorkDirectory, "artifacts"); err != nil {
		return err
	}

	if task.gcs, err = gsutil.NewClient(ctx, task.config.CloudStorage.CredentialsFile); err != nil {
		return err
	}

	return nil
}

func (task *SyncArtifactsTask) Run() error {
	remote := task.config.CloudStorage.ArtifactPath
	prefix := "*"
	if task.config.Fuzzer.UploadOnlyCrashes {
		prefix = "crash-*"
	}
	err := task.gcs.Copy(filepath.Join(task.artifactpath, prefix), remote, false)
	if err != nil {
		// Silence the error when there are no artifacts discovered yet.
		if strings.Contains(err.Error(), "No URLs matched") {
			return nil
		}
		return errors.New("failed to copy directory: " + err.Error())
	}
	return nil

}
