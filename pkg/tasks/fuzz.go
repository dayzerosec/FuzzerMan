package tasks

import (
	"FuzzerMan/pkg/cloudutil"
	"FuzzerMan/pkg/config"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type FuzzTask struct {
	config  *config.Config
	cloud   *cloudutil.Client
	context context.Context
}

func (task *FuzzTask) Initialize(ctx context.Context, cfg *config.Config) error {
	task.config = cfg
	task.context = ctx
	task.cloud = cloudutil.NewClient(ctx, task.config.CloudStorage.BucketURL)

	if info, err := os.Stat(cfg.FilePath(config.LocalFuzzerFile)); err != nil {
		return errors.New(fmt.Sprintf("unable to stat target binary: %s", err.Error()))
	} else {
		// Check for executable bit to be set on any of owner/group/everyone
		if info.Mode()&0111 == 0 {
			return errors.New("target binary is not executable")
		}
	}
	return nil

}

func (task *FuzzTask) Run() error {
	cloudCorpusPath := task.config.CloudPath(config.CorpusDirectory)
	localCorpusPath := task.config.WorkPath(config.CorpusDirectory)
	cloudLogPath := task.config.CloudPath(config.LogDirectory)
	localLogPath := task.config.WorkPath(config.LogDirectory)

	// Mirror the Corpus from the authority in the cloud into the local folder
	if err := task.cloud.MirrorLocal(cloudCorpusPath, localCorpusPath); err != nil {
		return errors.New(fmt.Sprintf("corpus mirror failed: %s", err.Error()))
	}

	startTime := time.Now()
	timestamp := startTime.UTC().Format("2006-01-02-150405.00000")
	logFilename := fmt.Sprintf("%s.log.txt", timestamp)
	if err := task.RunFuzzer(logFilename); err != nil {
		return err
	}

	log.Printf("[*] Uploading log: %s", logFilename)
	if err := task.cloud.Upload(localLogPath, []string{logFilename}, cloudLogPath); err != nil {
		log.Printf("[!] %s", err.Error())
	}

	if err := task.UploadNewCorpus(startTime); err != nil {
		log.Printf("[!] %s", err.Error())
	}
	if err := task.UploadNewArtifacts(startTime); err != nil {
		log.Printf("[!] %s", err.Error())
	}
	return nil
}

func (task *FuzzTask) writeLogHeader(writer io.Writer) (err error) {
	instance := task.config.InstanceId
	metadata := "" // not doing anything with this yet
	header := []byte(fmt.Sprintf("%s\n%s\n=====\n", instance, metadata))
	_, err = writer.Write(header)
	return
}

func (task *FuzzTask) RunFuzzer(logFilename string) error {
	localArtifactPath := task.config.WorkPath(config.ArtifactDirectory)
	localCorpusPath := task.config.WorkPath(config.CorpusDirectory)
	targetBinaryPath := task.config.FilePath(config.LocalFuzzerFile)
	localLogPath := task.config.WorkPath(config.LogDirectory)
	var args []string
	args = append(args, fmt.Sprintf("-fork=%d", task.config.Fuzzer.ForkCount))
	args = append(args, fmt.Sprintf("-max_total_time=%d", task.config.Fuzzer.MaxTotalTime))
	args = append(args, fmt.Sprintf("-artifact_prefix=%s/", localArtifactPath))
	args = append(args, task.config.Fuzzer.Arguments...)
	args = append(args, localCorpusPath)

	expectedDuration := time.Duration(task.config.Fuzzer.MaxTotalTime) * time.Second
	log.Printf("[*] Fuzzing for %d minutes. (%s)", int(expectedDuration.Minutes()), logFilename)
	cmd := exec.CommandContext(task.context, targetBinaryPath, args...)
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

	logFilePath := filepath.Join(localLogPath, logFilename)
	outfile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE, 0660)
	if err != nil {
		return err
	}
	defer func() { _ = outfile.Close() }()
	_ = task.writeLogHeader(outfile)

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
		log.Printf("[*] Killed by libFuzzer (OOM/Timeout)")
	case 1:
		log.Printf("[*] Got a crash")
		go task.ReportCrash(logFilePath)
	}
	return nil
}

func (task *FuzzTask) UploadNewCorpus(startTime time.Time) error {
	localCorpusPath := task.config.WorkPath(config.CorpusDirectory)
	cloudCorpusPath := task.config.CloudPath(config.CorpusDirectory)
	newCorpus, err := newFilesSince(localCorpusPath, startTime)
	if err != nil {
		return err
	}

	if len(newCorpus) > 0 {
		log.Printf("[*] New Corpus: %d", len(newCorpus))
		if err = task.cloud.Upload(localCorpusPath, newCorpus, cloudCorpusPath); err != nil {
			return err
		}
	}

	return nil
}

func (task *FuzzTask) UploadNewArtifacts(startTime time.Time) error {
	localArtifactPath := task.config.WorkPath(config.ArtifactDirectory)
	cloudArtifactPath := task.config.CloudPath(config.ArtifactDirectory)
	newArtifacts, err := newFilesSince(localArtifactPath, startTime)
	if err != nil {
		return err
	}

	if task.config.Fuzzer.UploadOnlyCrashes {
		var crashFiles []string
		for _, fn := range newArtifacts {
			if strings.HasPrefix(fn, "crash-") {
				crashFiles = append(crashFiles, fn)
			}
		}
		newArtifacts = crashFiles
	}

	if len(newArtifacts) > 0 {
		log.Printf("[*] New Artifacts: %d", len(newArtifacts))
		if err = task.cloud.Upload(localArtifactPath, newArtifacts, cloudArtifactPath); err != nil {
			return err
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
		log.Printf("[!] Unable find artifact file in %s", logfilePath)
		return
	}
	// Reset the log file descriptor so we can reuse it for the upload
	_, _ = logReader.Seek(0, 0)

	artifactReader, err := os.Open(filepath.Join(task.config.WorkPath(config.ArtifactDirectory), artifact))
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

func newFilesSince(dirname string, ts time.Time) ([]string, error) {
	var out []string
	files, err := ioutil.ReadDir(dirname)
	if err != nil {
		return out, err
	}

	for _, fn := range files {
		if fn.ModTime().After(ts) || fn.ModTime().Equal(ts) {
			out = append(out, fn.Name())
		}
	}
	return out, nil
}
