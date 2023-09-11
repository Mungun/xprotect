package config

import (
	"encoding/xml"
	"flag"
	"time"
)

type Config struct {
	BaseUrl  string
	ClientId string
	Username string
	Password string
	Port     string
}

func InitConfig() *Config {
	config := Config{}

	flag.StringVar(&config.Port, "port", ":9999", "server port")
	flag.StringVar(&config.BaseUrl, "baseUrl", "", "base addr of oauth2 endpoind")
	flag.StringVar(&config.ClientId, "clientId", "", "clientId")
	flag.StringVar(&config.Username, "user", "", "username")
	flag.StringVar(&config.Password, "password", "", "password")

	flag.Parse()

	// Const = &config

	return &config
}

type TokenResult struct {
	XMLName    xml.Name `xml:"Envelope"`
	Token      string   `xml:"Body>LoginResponse>LoginResult>Token"`
	TimeToLive int      `xml:"Body>LoginResponse>LoginResult>TimeToLive>MicroSeconds"`
	Expiry     time.Time
}
