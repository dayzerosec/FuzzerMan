package main

import (
	"FuzzerMan/pkg/config"
	"FuzzerMan/pkg/tasks"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/mroth/weightedrand/v2"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

var wg *sync.WaitGroup

// GenerateTaskConfig will merge the host and campaign configurations together to create a config like that
// expected by the tasks run by FuzzerMan
func GenerateTaskConfig(host config.HostConfig, campaign config.CampaignConfig, coreCount int) *config.Config {
	cfg := config.Config{
		InstanceId:        host.InstanceId,
		WorkDirectory:     path.Join(host.WorkDirectory, campaign.Id),
		ReportingEndpoint: campaign.ReportingEndpoint,
		InitScript:        "",
		CloudStorage:      campaign.CloudStorage,
		Fuzzer: config.FuzzerConfig{
			ForkCount:         coreCount,
			MaxTotalTime:      campaign.MaxTotalTime,
			IncludeHostEnv:    campaign.IncludeHostEnv,
			Arguments:         campaign.Arguments,
			Environment:       campaign.Environment,
			UploadOnlyCrashes: campaign.UploadOnlyCrashes,
		},
	}
	if host.EnableMergeTask && campaign.MergeInterval > 0 {
		cfg.MergeTask.Enabled = true
		cfg.MergeTask.Interval = campaign.MergeInterval
	} else {
		cfg.MergeTask.Enabled = false
	}
	return &cfg
}

// runCampaignUntil continues to run the fuzzer in forking mode until the time has been reached
// as LibFuzzer may overrun the max time a bit this is not a perfect scheduler just a rough guideline
// it will return immediately if there are any early errors but will try again for errors while fuzzing
func runCampaignUntil(end time.Time, cfg *config.Config) error {
	defer wg.Done()

	if _, err := os.Stat(cfg.WorkDirectory); os.IsNotExist(err) {
		_ = os.MkdirAll(cfg.WorkDirectory, 0770)
	}

	syncBinary := tasks.SyncTargetBinaryTask{}
	if err := syncBinary.Initialize(context.Background(), cfg); err != nil {
		return err
	}
	if err := syncBinary.Run(); err != nil {
		return err
	}

	if cfg.MergeTask.Enabled {
		mergeTask := tasks.CorpusMergeTask{}
		if err := mergeTask.Initialize(context.Background(), cfg); err != nil {
			return err
		}
		if err := mergeTask.Run(); err != nil {
			return err
		}
	}

	fuzzTask := tasks.FuzzTask{}
	if err := fuzzTask.Initialize(context.Background(), cfg); err != nil {
		return err
	}

	// We'll run the fuzzer until time is up, but if we are within 5-minutes of the end time don't bother
	for time.Now().Before(end.Add(-5 * time.Minute)) {
		// Limit run-time to the remaining time if necessary
		remaining := int(end.Sub(time.Now()).Seconds())
		if remaining < cfg.Fuzzer.MaxTotalTime {
			cfg.Fuzzer.MaxTotalTime = remaining
		}

		if err := fuzzTask.Run(); err != nil {
			// If it got here fine, lets let it run for its full period before returning even with errors
			log.Printf("Failed to run fuzz task: %s", err.Error())
			time.Sleep(15 * time.Second)
		}
	}
	return nil
}

// GetCampaigns fetches the latest campaign listing either from a file or website
func GetCampaigns(endpoint string) (map[string]config.CampaignConfig, error) {
	var content []byte
	var err error
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		client := http.Client{
			Timeout: time.Second * 15,
		}
		res, err := client.Get(endpoint)
		if err != nil {
			return nil, err
		}
		defer func() { _ = res.Body.Close() }()

		if res.StatusCode != 200 {
			return nil, fmt.Errorf("campaign endpoint got a non-200 error code: %d", res.StatusCode)
		}

		content, err = io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
	} else {
		content, err = os.ReadFile(endpoint)
		if err != nil {
			return nil, err
		}
	}

	var campaigns []config.CampaignConfig
	if err = json.Unmarshal(content, &campaigns); err != nil {
		return nil, err
	}

	out := make(map[string]config.CampaignConfig)
	for _, c := range campaigns {
		out[c.Id] = c
	}
	return out, nil
}

// generateCoreSplit takes into account the weights in the campaign config and splits tasks across cores
func generateCoreSplit(campaigns map[string]config.CampaignConfig, host config.HostConfig) map[string]int {
	jobs := make(map[string]int)
	choices := make([]weightedrand.Choice[config.CampaignConfig, int], len(campaigns))
	for _, c := range campaigns {
		choices = append(choices, weightedrand.NewChoice(c, c.Weight))
		jobs[c.Id] = 0
	}

	bucket, err := weightedrand.NewChooser(choices...)
	if err != nil {
		panic(err)
	}

	for i := 0; i < host.MaxJobCount; i++ {
		choice := bucket.Pick()
		jobs[choice.Id]++
	}

	return jobs
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
	wg = &sync.WaitGroup{}
}

func main() {
	var cfg config.MultiConfig
	cfn := flag.String("config", "", "location of MultiConfig config file")
	flag.Parse()

	if content, err := os.ReadFile(*cfn); err != nil {
		panic(err)
	} else {
		if err = json.Unmarshal(content, &cfg); err != nil {
			panic(err)
		}
	}

	campaigns, err := GetCampaigns(cfg.CampaignSource)
	if err != nil {
		panic(err)
	}

	for {
		if newCampaigns, err := GetCampaigns(cfg.CampaignSource); err == nil {
			// Refresh campaigns every loop, but if it fails just use the old one
			campaigns = newCampaigns
		}

		splits := generateCoreSplit(campaigns, cfg.Host)

		// Calculating how long we will be running for based on the shorted MaxTotalTime value
		var endTime time.Time
		now := time.Now()
		for _, c := range campaigns {
			// If we are not running the job it shouldn't influence the endTime
			if splits[c.Id] == 0 {
				continue
			}

			campaignDuration := time.Duration(c.MaxTotalTime) * time.Second
			if endTime.IsZero() || now.Add(campaignDuration).Before(endTime) {
				endTime = now.Add(campaignDuration)
			}
		}

		for k, v := range splits {
			if v == 0 {
				continue
			}
			c := campaigns[k]
			log.Printf("[%s] Cores: %d", c.Id, v)
			taskConfig := GenerateTaskConfig(cfg.Host, c, v)

			wg.Add(1)
			go func() {
				if err = runCampaignUntil(endTime, taskConfig); err != nil {
					log.Printf("[%s] ERR: %s", c.Id, err.Error())
				}
			}()
		}
		wg.Wait()
	}

}
