package tasks

import (
	"FuzzerMan/pkg/cloudutil"
	"FuzzerMan/pkg/config"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

type CorpusMergeTask struct {
	config  *config.Config
	cloud   *cloudutil.Client
	context context.Context
}

func (task *CorpusMergeTask) Initialize(ctx context.Context, cfg *config.Config) error {
	var err error
	task.config = cfg
	task.context = ctx
	task.cloud = cloudutil.NewClient(ctx, task.config.CloudStorage.BucketURL)

	// Ensure the merge lock exists and the expected Cache-Control value
	lockAttrs, err := task.cloud.FileInfo(task.config.FilePath(config.MergeLockFile))
	if (err != nil && gcerrors.Code(err) == gcerrors.NotFound) || lockAttrs.CacheControl != "no-cache" {
		opts := &blob.WriterOptions{CacheControl: "no-cache"}
		if err = task.cloud.WriteFile(task.config.FilePath(config.MergeLockFile), []byte("---"), opts); err != nil {
			return errors.New(fmt.Sprintf("failed to create merge lock: %s", err.Error()))
		}
	}

	return nil
}

func (task *CorpusMergeTask) ShouldMerge() bool {
	if !task.config.MergeTask.Enabled {
		return false
	}
	lockPath := task.config.FilePath(config.MergeLockFile)

	info, err := task.cloud.FileInfo(lockPath)
	if err != nil {
		return false
	}

	timeSinceMerge := time.Now().Sub(info.ModTime)
	if int(timeSinceMerge.Seconds()) > task.config.MergeTask.Interval {
		log.Printf("[*] Attempting to grab merge lock (last merge: %.2fh)", timeSinceMerge.Hours())

		uid := uuid.New().String()
		err := task.cloud.WriteFile(lockPath, []byte(uid), &blob.WriterOptions{CacheControl: "no-cache"})
		if err != nil {
			return false
		}

		// This sleep is just to give time for any competing writes to finish up
		// anything new should see the new updated time and not even try
		time.Sleep(time.Second * 15)

		content, err := task.cloud.ReadFile(lockPath, nil)
		if err != nil {
			log.Printf("[!] Failed to read lock file to confirm lock holder: %s", err.Error())
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
	localCorpusPath := task.config.WorkPath(config.CorpusDirectory)
	cloudCorpusPath := task.config.CloudPath(config.CorpusDirectory)

	log.Println("[*] Creating temporary corpus directory")
	tempCorpus := task.config.WorkPath(config.TempDirectory)
	defer func() { _ = os.RemoveAll(tempCorpus) }()

	log.Println("[*] Mirroring corpus")
	if err := task.cloud.MirrorLocal(cloudCorpusPath, localCorpusPath); err != nil {
		return errors.New(fmt.Sprintf("corpus mirror failed: %s", err.Error()))
	}

	// Run the actual merge job
	log.Println("[*] Running merge")
	var args []string
	args = append(args, "-merge=1")
	args = append(args, task.config.Fuzzer.Arguments...)
	args = append(args, tempCorpus, task.config.WorkPath(config.CorpusDirectory))
	cmd := exec.CommandContext(task.context, task.config.FilePath(config.LocalFuzzerFile), args...)
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
	var newKeys []string
	var newObjects []*blob.ListObject
	newObjects, err = task.cloud.NewObjects(cloudCorpusPath, startTime)
	if err != nil {
		return fmt.Errorf("failed to get new object list: %s", err.Error())
	}
	if len(newObjects) > 0 {
		for _, obj := range newObjects {
			newKeys = append(newKeys, obj.Key)
		}
		if err := task.cloud.Download(newKeys, tempCorpus); err != nil {
			return fmt.Errorf("failed to copy new files into merged corpus: %s", err.Error())
		}
	}

	if err := task.cloud.MirrorRemote(tempCorpus, cloudCorpusPath); err != nil {
		return errors.New(fmt.Sprintf("corpus mirror failed: %s", err.Error()))
	}

	// Now we are done for real, update the lockfile again just to update the modified time
	_ = task.cloud.WriteFile(task.config.FilePath(config.MergeLockFile), []byte("---"), &blob.WriterOptions{CacheControl: "no-cache"})

	timeConsumed := time.Now().Sub(startTime)
	if int(timeConsumed.Seconds()) > task.config.MergeTask.Interval {
		log.Printf("WARNING: Merge task took %d seconds.", int(timeConsumed.Seconds()))
	}

	return nil
}
