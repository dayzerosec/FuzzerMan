package config

type CampaignConfig struct {
	// ID should be filesystem safe as it is used to find the work directory
	Id                string
	ReportingEndpoint string
	CloudStorage      CloudStorageConfig
	MaxTotalTime      int
	IncludeHostEnv    bool
	Arguments         []string
	Environment       []string
	UploadOnlyCrashes bool
	MergeInterval     int
	Weight            int
}

type HostConfig struct {
	InstanceId      string
	MaxJobCount     int
	WorkDirectory   string
	EnableMergeTask bool
}

type MultiConfig struct {
	Host           HostConfig
	CampaignSource string
}
