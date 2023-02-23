package tasks

import (
	"FuzzerMan/pkg/config"
	"path/filepath"
	"testing"
)

func TestReportCrash(t *testing.T) {
	task := FuzzTask{
		config: &config.Config{
			InstanceId:        "",
			WorkDirectory:     "C:\\Users\\zi\\GolandProjects\\FuzzerMan\\local\\working",
			ReportingEndpoint: "https://reports.tools.cyantom.com/rjvyt9fLWuVWEwSTnSRnYr4C/mali-v518/report",
			InitScript:        "",
			CloudStorage:      config.CloudStorageConfig{},
			Fuzzer:            config.FuzzerConfig{},
			MergeTask: struct {
				Enabled  bool
				Interval int
			}{},
		},
		cloud:   nil,
		context: nil,
	}

	logs := []string{
		//"2023-02-20-151452.51879.log.txt",
		//"2023-02-21-143642.77224.log.txt",
		//"2023-02-22-002850.59157.log.txt",
		//"2023-02-23-062016.11789.log.txt",
	}

	for _, logfn := range logs {
		task.ReportCrash(filepath.Join(task.config.WorkPath(config.LogDirectory), logfn))
	}
}
