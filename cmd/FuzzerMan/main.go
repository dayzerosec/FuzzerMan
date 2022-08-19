package main

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/tasks"
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
)

func main() {
	configfn := flag.String("config", "", "Path to configuration file")
	flag.Parse()
	cfg, err := config.Load(*configfn)
	if err != nil {
		log.Printf("failed to load configuration: %s", err.Error())
		os.Exit(1)
	}

	cmd := exec.Command("gcloud", "auth", "activate-service-account", "--key-file", cfg.CloudStorage.CredentialsFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("failed to run `gcloud auth`: %s", err.Error())
		os.Exit(1)
	}
	if cmd.ProcessState.ExitCode() != 0 {
		log.Printf("[!] `gcloud auth` gave a non-zero exit code (%d)", cmd.ProcessState.ExitCode())
		log.Println(out)
		os.Exit(1)
	}

	log.Println(cfg.InstanceId)
	taskList := []tasks.RunnableTask{
		&tasks.SyncTargetBinaryTask{},
		&tasks.FuzzTask{},
		&tasks.SyncArtifactsTask{},
		&tasks.SyncLogTask{},
		&tasks.CorpusMergeTask{},
	}
	c := context.Background()

	for _, t := range taskList {
		if err := t.Initialize(c, cfg); err != nil {
			panic(err)
		}

		// We need to sync the target binary early or other task init routines will fail
		if _, ok := t.(*tasks.SyncTargetBinaryTask); ok {
			if err := t.Run(); err != nil {
				panic(err)
			}
		}
	}

	for {
		for _, t := range taskList {
			if err := t.Run(); err != nil {
				log.Printf("[!] ERROR: %s", err.Error())
			}
		}
	}
}
