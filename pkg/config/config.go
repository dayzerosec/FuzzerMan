package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
)

type Config struct {
	// InstanceId can be any string, it will be prepended to every log this fuzzer instance produces
	InstanceId string
	// WorkDirectory is a directory for any files the instance needs to store namely corpus, artifacts and logs
	WorkDirectory string
	// ReportingEndpoint is an optional location for reporting crashes. A multipart/form-data POST request will be made
	// to this endpoint with two fields, `log` and `artifact` containing the entirety of the log file and crash artifact.
	ReportingEndpoint string
	// InitScript should be the full path to an executable. Can use this to do any init work like setting up default auth
	// the first and only argument is the configuration filename
	InitScript string
	// CloudStorage contains all the URLs for cloud locations. The Paths should be relative to the root of the bucket. eg `example-campaign/corpus`
	CloudStorage struct {
		// BucketURL is the URL with the service specific schema to your storage bucket (ex. gs://my-bucket).
		BucketURL string
		// Prefix is the relative path from the bucket to where campaign files should be stored (ex. campaigns/my-specific-campaign)
		Prefix string
	}
	// Fuzzer is all the configuration options for Fuzz jobs
	Fuzzer struct {
		// ForkCount is the argument to -fork=N, core count is a good starting place for this value
		ForkCount int
		// MaxTotalTime represents the `-max_total_time` argument
		MaxTotalTime int
		// IncludeHostEnv indicates whether the host's environment variables should be passed into the target binary
		// argument is an integer reflecting the number of seconds to run for
		IncludeHostEnv bool
		// Arguments are passed into the fuzzer as is. So any libFuzzer arguments are value
		Arguments []string
		// Environment these are passed in regardless of the `IncludeHostEnv` value. You can use this to overwrite any
		// existing environment vars or add some new ones
		Environment []string
		// UploadOnlyCrashes libFuzzer will report on other types of issues like slow runs and oom. You may want to
		// ignore those and only upload `crash-*` artifacts. Enable this to do so.
		UploadOnlyCrashes bool
	}
	// MergeTask is configuration specifically for the merge task
	MergeTask struct {
		// Enabled determines if you want this instance to even attempt to do the merge.
		Enabled bool
		// Interval in seconds between merge attempts. This interval should be longer than a merge attempt to prevent
		// any potential corpus collisions and clobbering. There is some simple synchronization to ensure only one instance
		// attempt a merge at a time.
		Interval int
	}
}

func Load(fn string) (*Config, error) {
	if fn == "" {
		return nil, errors.New("Missing configuration file.")
	}

	content, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to load configuration: %s", err.Error()))
	}

	var out Config
	if err = json.Unmarshal(content, &out); err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to load configuration: %s", err.Error()))
	}

	return &out, nil
}
