package config

// Composing structs for unmarshalling. Writer is lumberjack's setup struct.
// It's annotated for JSON out-of-the-box.
// Logrus is for logging level and log output settings.
type AppenderType string

const (
	RollingFile AppenderType = "rolling_file"
	Console     AppenderType = "console"
)

/*type GlobalConfig struct {
	Logging Logging `json:"logging"`
}*/

type DestinationConfig struct {
	File       AppenderType `json:"file"`
	BufferSize int          `json:"buffer_size"`
	MaxSize    int          `json:"max_size"`
	MaxAge     int          `json:"max_age"`
	MaxBackups int          `json:"max_backups"`
	LocalTime  bool         `json:"local_time"`
}

type AppenderConfig struct {
	Type        AppenderType       `json:"type"`
	Enabled     bool               `json:"enabled"`
	Level       string             `json:"level"`
	Destination *DestinationConfig `json:"destination"`
}

type LoggingConfig struct {
	Level          string           `json:"level"`
	Appenders      []AppenderConfig `json:"appenders"`
	ConsoleOutHTTP bool             `json:"http_console_out"`
	LogHTTP        bool             `json:"http_log"`
}

func (cfg *LoggingConfig) Validate() error {
	return nil
}
