package tasks

import (
	"FuzzerMan/pkg/cloudutil"
	"FuzzerMan/pkg/config"
	"context"
	"errors"
	"log"
	"os"
	"time"
)

type SyncTargetBinaryTask struct {
	config  *config.Config
	cloud   *cloudutil.Client
	context context.Context
}

func (task *SyncTargetBinaryTask) Initialize(ctx context.Context, cfg *config.Config) error {
	task.config = cfg
	task.context = ctx
	task.cloud = cloudutil.NewClient(task.context, cfg.CloudStorage.BucketURL)

	// Check that the fuzzer exists on the cloud
	if _, err := task.cloud.FileInfo(task.config.FilePath(config.CloudFuzzerFile)); err != nil {
		return err
	}
	return nil
}

func (task *SyncTargetBinaryTask) Run() error {
	localpath := task.config.FilePath(config.LocalFuzzerFile)
	remotepath := task.config.FilePath(config.CloudFuzzerFile)

	var localts, remotets time.Time
	info, err := os.Stat(localpath)
	if err == nil {
		localts = info.ModTime()
	} else {
		if !os.IsNotExist(err) {
			return errors.New("failed to stat fuzzer: " + err.Error())
		}
		// fall through path will leave localts set to epoch
	}

	object, err := task.cloud.FileInfo(remotepath)
	if err != nil {
		return errors.New("failed to get remote modification time for target binary: " + err.Error())
	}
	remotets = object.ModTime

	if remotets.Before(localts) || remotets.Equal(localts) {
		return nil
	}

	log.Printf("[*] Fetching updated target binary.")
	if err = task.cloud.DownloadSingle(remotepath, localpath); err != nil {
		return errors.New("failed to fetch target binary: " + err.Error())
	}

	if err = os.Chmod(localpath, 0770); err != nil {
		return errors.New("failed to chmod target binary: " + err.Error())
	}

	log.Printf("[*] Updated target binary")
	return nil
}
