package service

type Config struct {
	LogFilePath      string `yaml:"log_filepath"`
	MaxFileSizeBytes int    `yaml:"max_filesize_bytes"`
	ActiveMode       bool   `yaml:"active_mode"`
	MilterHost       string `yaml:"milter_host"`
	MilterPort       string `yaml:"milter_port"`
	ThorThunderStorm string `yaml:"thorthunderstorm_url"`
	Expression       string `yaml:"quarantine_expression"`
}
