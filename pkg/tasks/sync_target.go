package tasks

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/gsutil"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"
)

type SyncTargetBinaryTask struct {
	config  *config.Config
	gsutil  *gsutil.Client
	context context.Context
}

func (task *SyncTargetBinaryTask) Initialize(ctx context.Context, config *config.Config) error {
	var err error
	task.config = config
	task.context = ctx

	if config.CloudStorage.CredentialsFile == "" {
		return errors.New("missing credentials file")
	}

	if config.CloudStorage.TargetPath == "" {
		return errors.New("missing cloud storage target path")
	}

	task.gsutil, err = gsutil.NewClient(task.context, config.CloudStorage.CredentialsFile)
	if err != nil {
		return err
	}

	return nil
}

func (task *SyncTargetBinaryTask) Run() error {
	localpath := filepath.Join(task.config.WorkDirectory, "target")
	remotepath := task.config.CloudStorage.TargetPath

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

	object, err := task.gsutil.FileInfo(remotepath)
	if err != nil {
		return errors.New("failed to get remote modification time for target binary: " + err.Error())
	}
	remotets = object.Updated

	if remotets.Before(localts) || remotets.Equal(localts) {
		return nil
	}

	log.Printf("[*] Fetching updated target binary.")
	if err = task.gsutil.Copy(remotepath, localpath, false); err != nil {
		return errors.New("failed to fetch target binary: " + err.Error())
	}

	if err = os.Chmod(localpath, 0770); err != nil {
		return errors.New("failed to chmod target binary: " + err.Error())
	}

	log.Printf("[*] Updated target binary")
	return nil
}
