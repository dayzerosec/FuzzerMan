package main

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/tasks"
	"context"
	"errors"
	"flag"
	"fmt"
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

	if err = testConfig(cfg); err != nil {
		log.Printf("Invalid configuration: %s", err.Error())
		os.Exit(1)
	}

	if cfg.InitScript != "" {
		cmd := exec.Command(cfg.InitScript, *configfn)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("failed to run '%s'", cfg.InitScript)
			log.Println(string(out))

			os.Exit(1)
		}
	}

	log.Printf("Running as: %s", cfg.InstanceId)

	taskList := []tasks.RunnableTask{
		&tasks.FuzzTask{},
		&tasks.CorpusMergeTask{},
		&tasks.SyncTargetBinaryTask{},
	}
	c := context.Background()

	for _, t := range taskList {
		if err := t.Initialize(c, cfg); err != nil {
			panic(err)
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

func testConfig(c *config.Config) error {
	// Check we Have all the necessary local folders:
	localFolders := []config.DirectoryName{config.CorpusDirectory, config.ArtifactDirectory, config.LogDirectory}
	for _, name := range localFolders {
		path := c.WorkPath(name)
		if info, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				log.Printf("Creating Directory: %s", path)
				if err = os.MkdirAll(path, 0770); err != nil {
					return err
				}
			}
		} else {
			if !info.IsDir() {
				return fmt.Errorf("'%s' exists but is not a directory", path)
			}
		}
	}

	// Runs the Sync Target Binary task early since it is needed for all the other tasks
	syncBinary := tasks.SyncTargetBinaryTask{}
	if err := syncBinary.Initialize(context.Background(), c); err != nil {
		return err
	}
	if err := syncBinary.Run(); err != nil {
		return err
	}

	// Ensure the target binary is executable
	if info, err := os.Stat(c.FilePath(config.LocalFuzzerFile)); err != nil {
		return errors.New(fmt.Sprintf("unable to stat target binary: %s", err.Error()))
	} else {
		// Check for executable bit to be set on any of owner/group/everyone
		if info.Mode()&0111 == 0 {
			return errors.New("target binary is not executable")
		}
	}

	return nil
}
