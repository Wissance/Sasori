package config

import (
	"errors"
	"os"
	"time"

	sf "github.com/wissance/stringFormatter"
)

type Schema string

const (
	HTTP  Schema = "http"
	HTTPS Schema = "https"
)

type SecurityConfig struct {
	CertificateFile string `json:"certificate_file" example:"./certificates/server.crt"`
	KeyFile         string `json:"key_file" example:"./certificates/server.key"`
}

type ServerConfig struct {
	Schema          Schema          `json:"schema" example:"http or https"`
	Address         string          `json:"address" example:"127.0.0.1 or mydomain.com"`
	Port            int             `json:"port" example:"8080"`
	Security        *SecurityConfig `json:"security"`
	SecretFile      string          `json:"secret_file" example:"./keyfile"`
	ShutdownTimeout time.Duration   `json:"shutdown_timeout"`
}

func (cfg *ServerConfig) Validate() error {
	if len(cfg.SecretFile) == 0 {
		return errors.New("secret file wasn't set")
	}
	_, err := os.Stat(cfg.SecretFile)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return errors.New(sf.Format("secret file on path \"{0}\" does not exists", cfg.SecretFile))
	}
	if cfg.Schema == HTTPS {
		if cfg.Security == nil {
			return errors.New("https schema requires a certs pair (\"security\" property)")
		}

		_, keyFileErr := os.Stat(cfg.Security.KeyFile)
		if keyFileErr != nil && errors.Is(keyFileErr, os.ErrNotExist) {
			return errors.New(sf.Format("Security (certificate) config Key file \"{0}\" does not exists", cfg.Security.KeyFile))
		}

		_, crtFileErr := os.Stat(cfg.Security.CertificateFile)
		if crtFileErr != nil && errors.Is(crtFileErr, os.ErrNotExist) {
			return errors.New(sf.Format("Security (certificate) config Certificate file \"{0}\" does not exists", cfg.Security.CertificateFile))
		}
	}
	return nil
}
