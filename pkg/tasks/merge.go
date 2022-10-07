package tasks

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/gsutil"
	"cloud.google.com/go/storage"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CorpusMergeTask struct {
	config       *config.Config
	gcs          *gsutil.Client
	context      context.Context
	mirrorCorpus string
	targetPath   string
}

func (task *CorpusMergeTask) Initialize(ctx context.Context, config *config.Config) error {
	var err error
	task.config = config
	task.context = ctx

	if config.CloudStorage.CredentialsFile == "" {
		return errors.New("missing credentials file")
	}

	if !strings.HasPrefix(config.CloudStorage.CorpusPath, "gs://") {
		return errors.New("cloud corpus path must start with gs://")
	}

	if task.mirrorCorpus, err = GetWorkDir(config.WorkDirectory, "corpus", "cloud"); err != nil {
		return err
	}

	task.targetPath = filepath.Join(config.WorkDirectory, "target")
	if info, err := os.Stat(task.targetPath); err != nil {
		return errors.New(fmt.Sprintf("unable to stat target binary: %s", err.Error()))
	} else {
		// Check for executable bit to be set on any of owner/group/everyone
		if info.Mode()&0111 == 0 {
			return errors.New("target binary is not executable")
		}
	}

	if task.gcs, err = gsutil.NewClient(ctx, task.config.CloudStorage.CredentialsFile); err != nil {
		return err
	}

	// Ensure the merge lock exists and the expected Cache-Control value
	lockObject, err := task.gcs.Object(task.config.CloudStorage.MergeLockPath)
	if err != nil {
		return err
	}

	lockAttrs, err := lockObject.Attrs(task.context)
	if err == storage.ErrObjectNotExist {
		if err = task.gcs.WriteFile(task.config.CloudStorage.MergeLockPath, []byte("---")); err != nil {
			return errors.New(fmt.Sprintf("failed to create merge lock: %s", err.Error()))
		}
		// replace attrs and err vars and follow the same flow as if it always existed
		lockAttrs, err = lockObject.Attrs(task.context)
	}

	if err != nil {
		return err
	}
	if lockAttrs.CacheControl != "no-cache" {
		_, err = lockObject.Update(task.context, storage.ObjectAttrsToUpdate{CacheControl: "no-cache"})
		if err != nil {
			// Seems that Google will wrongly indicate CacheControl is empty, and then realize it had the wrong value
			// when we try to update it. Providing an Error 409 that indicates the metadata was updated by someone else
			if !strings.Contains(err.Error(), "Error 409:") {
				return errors.New(fmt.Sprintf("failed to update lock cache control: %s", err.Error()))
			}
		}
	}

	return nil
}

func (task *CorpusMergeTask) ShouldMerge() bool {
	if !task.config.MergeTask.Enabled {
		return false
	}

	info, err := task.gcs.FileInfo(task.config.CloudStorage.MergeLockPath)
	if err != nil {
		return false
	}

	timeSinceMerge := time.Now().Sub(info.Updated)
	if int(timeSinceMerge.Seconds()) > task.config.MergeTask.Interval {
		log.Printf("[*] Attempting to grab merge lock", timeSinceMerge.Hours())

		uid := uuid.New().String()
		err := task.gcs.WriteFile(task.config.CloudStorage.MergeLockPath, []byte(uid))
		if err != nil {
			return false
		}

		// This sleep is just to give time for any competing writes to finish up
		// anything new should see the new updated time and not even try
		time.Sleep(time.Second * 15)

		content, err := task.gcs.ReadFile(task.config.CloudStorage.MergeLockPath)
		if err != nil {
			log.Printf("[!] Failed to read lock file to confirm: %s", err.Error())
			return false
		}

		return string(content) == uid
	}
	return false
}

func (task *CorpusMergeTask) Run() error {
	if !task.ShouldMerge() {
		return nil
	}
	startTime := time.Now()

	log.Println("[*] Creating temporary corpus directory")
	tempCorpus, err := GetWorkDir(task.config.WorkDirectory, "corpus", "temp")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempCorpus) }()

	log.Println("[*] Mirroring corpus")
	if err := task.gcs.Mirror(task.config.CloudStorage.CorpusPath, task.mirrorCorpus, false); err != nil {
		return errors.New(fmt.Sprintf("corpus mirror failed: %s", err.Error()))
	}

	// Run the actual merge job
	log.Println("[*] Running merge")
	var args []string
	args = append(args, "-merge=1")
	args = append(args, task.config.Fuzzer.Arguments...)
	args = append(args, tempCorpus, task.mirrorCorpus)
	cmd := exec.CommandContext(task.context, task.targetPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(out))
		log.Printf("Merged failed: %s", err.Error())
		return err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "MERGE-OUTER: ") {
			log.Println(line)
		}
	}

	// Done but we don't want to lose any corpus that was added while doing the merge
	// So lets grab all those new files since we started, and then try and copy them into tempCorpus
	// then Mirror tempCorpus into the authoritative location
	var newFileArgs []string
	var newFiles []*storage.ObjectAttrs
	newFiles, err = task.gcs.NewObjects(task.config.CloudStorage.CorpusPath, startTime)
	if err != nil {
		return fmt.Errorf("failed to get new object list: %s", err.Error())
	}
	if len(newFiles) > 0 {
		for _, attrs := range newFiles {
			newFileArgs = append(newFileArgs, fmt.Sprintf("gs://%s/%s", attrs.Bucket, attrs.Name))
		}
		if err := task.gcs.CopyMulti(newFileArgs, tempCorpus, false); err != nil {
			return fmt.Errorf("failed to copy new files into merged corpus: %s", err.Error())
		}
	}
	if err := task.gcs.Mirror(tempCorpus, task.config.CloudStorage.CorpusPath, false); err != nil {
		return errors.New(fmt.Sprintf("corpus mirror failed: %s", err.Error()))
	}

	// Now we are done for real, update the lockfile again just to update the modified time
	_ = task.gcs.WriteFile(task.config.CloudStorage.MergeLockPath, []byte("---"))

	timeConsumed := time.Now().Sub(startTime)
	if int(timeConsumed.Seconds()) > task.config.MergeTask.Interval {
		log.Printf("WARNING: Merge task took %d seconds.", int(timeConsumed.Seconds()))
	}

	return nil
}
