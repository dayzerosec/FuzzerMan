package tasks

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/gsutil"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type FuzzTask struct {
	config       *config.Config
	gcs          *gsutil.Client
	context      context.Context
	newCorpus    string
	mirrorCorpus string
	targetPath   string
	artifactPath string
	logPath      string
}

func (task *FuzzTask) Initialize(ctx context.Context, config *config.Config) error {
	var err error
	task.config = config
	task.context = ctx

	if config.CloudStorage.LogPath == "" {
		return errors.New("missing log path")
	}
	if config.CloudStorage.CorpusPath == "" {
		return errors.New("missing corpus path")
	}

	if task.newCorpus, err = GetWorkDir(config.WorkDirectory, "corpus", "new"); err != nil {
		return err
	}

	if task.mirrorCorpus, err = GetWorkDir(config.WorkDirectory, "corpus", "cloud"); err != nil {
		return err
	}

	if task.artifactPath, err = GetWorkDir(config.WorkDirectory, "artifacts"); err != nil {
		return err
	}

	if task.logPath, err = GetWorkDir(config.WorkDirectory, "logs"); err != nil {
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

	return nil

}

func (task *FuzzTask) writeHeader(writer io.Writer) (err error) {
	instance := task.config.InstanceId
	metadata := "" // not doing anything with this yet
	header := []byte(fmt.Sprintf("%s\n%s\n=====\n", instance, metadata))
	_, err = writer.Write(header)
	return
}

func (task *FuzzTask) Run() error {
	// 1. Mirror the cloud corpus in work/corpus/cloud
	//    - As far as we are concerned the cloud is the authority
	// 2. Run the fuzzer, it will write new corpus into work/corpus/new
	//    -  Two folders are used a form of fault tolerance. If uploading of new corpus
	//       fails we don't lose them when we re-mirror the cloud corpus on the next run.
	// 3. Upload any new corpus files to the cloud
	//    - We do this in the fuzz task because we also mirror the corpus from this task
	//    - A cron should run to minimize the cloud corpus occasionally
	// 4. Clean up the work/corpus/new folder

	/*
		log.Println("[*] Mirroring corpus")
		if err := task.gcs.Mirror(task.config.CloudStorage.CorpusPath, task.mirrorCorpus, false); err != nil {
			return errors.New(fmt.Sprintf("corpus mirror failed: %s", err.Error()))
		}

	*/

	var args []string
	args = append(args, fmt.Sprintf("-fork=%d", task.config.Fuzzer.ForkCount))
	args = append(args, fmt.Sprintf("-max_total_time=%d", task.config.Fuzzer.MaxTotalTime))
	args = append(args, fmt.Sprintf("-artifact_prefix=%s/", task.artifactPath))
	args = append(args, task.config.Fuzzer.Arguments...)

	// First Corpus is where it will write new content, second and following are just used to merge into first
	args = append(args, task.newCorpus, task.mirrorCorpus)

	expectedDuration := time.Duration(task.config.Fuzzer.MaxTotalTime) * time.Second
	log.Printf("[*] Executing fuzzer for %d minutes.", int(expectedDuration.Minutes()))
	cmd := exec.CommandContext(task.context, task.targetPath, args...)

	if task.config.Fuzzer.IncludeHostEnv {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, task.config.Fuzzer.Environment...)

	//libFuzzer prints its info to stderr only
	outPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	timestamp := time.Now().UTC().Format("2006-01-02-150405.00000")
	logFilePath := filepath.Join(task.logPath, fmt.Sprintf("%s.log.txt", timestamp))
	log.Printf("[*] Logging fuzzer output to %s", logFilePath)

	outfile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE, 0660)
	if err != nil {
		return err
	}
	defer func() { _ = outfile.Close() }()
	_ = task.writeHeader(outfile)

	buf := make([]byte, 2048)
	for {
		n, err := outPipe.Read(buf)
		if n > 0 {
			_, _ = outfile.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	_ = outfile.Close()
	_ = cmd.Wait()

	switch cmd.ProcessState.ExitCode() {
	case 77:
		// This is usually an OOM/Timeout "crash"
		log.Printf("[*] Killed by libFuzzer")
	case 1:
		log.Printf("[*] Got a crash")
		go task.ReportCrash(logFilePath)
	}

	log.Printf("[*] Backing up new corpus files")
	if err := task.gcs.Copy(filepath.Join(task.newCorpus, "*"), task.config.CloudStorage.CorpusPath, false); err != nil {
		log.Println(err.Error())
		return err
	}

	// Clear the new folder so we know whats new next time
	if files, err := os.ReadDir(task.newCorpus); err == nil {
		for _, fn := range files {
			if fn.IsDir() {
				continue
			}
			// It's okay if this fails, it'll hopefully get picked up next round
			_ = os.Remove(filepath.Join(task.newCorpus, fn.Name()))
		}
	}
	return nil
}

func (task *FuzzTask) ReportCrash(logfilePath string) {
	if task.config.ReportingEndpoint == "" {
		return
	}

	// first we need the artifact
	targetString := "Test unit written to "
	artifact := ""

	logReader, err := os.OpenFile(logfilePath, os.O_RDONLY, 0660)
	if err != nil {
		log.Printf("[!] Failed to open %s: %s", logfilePath, err.Error())
		return
	}
	defer func() { _ = logReader.Close() }()

	scanner := bufio.NewScanner(logReader)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, targetString) {
			artifact = strings.Split(line, targetString)[1]
			artifact = filepath.Base(strings.TrimSpace(artifact))
			break
		}
	}
	if artifact == "" {
		log.Println("[!] Unable find artifact file in %s", logfilePath)
		return
	}
	// Reset the log file descriptor so we can reuse it for the upload
	_, _ = logReader.Seek(0, 0)

	artifactReader, err := os.Open(filepath.Join(task.artifactPath, artifact))
	if err != nil {
		log.Printf("[!] Failed to open artifact file: %s", err.Error())
		return
	}
	defer func() { _ = artifactReader.Close() }()

	if err = MultipartFileUpload(&http.Client{Timeout: 5 * time.Minute}, task.config.ReportingEndpoint, map[string]io.Reader{
		"log":      logReader,
		"artifact": artifactReader,
	}); err != nil {
		log.Printf("[!] Crash report failed: %s", err.Error())
	}
}
