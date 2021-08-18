package util

import (
	"encoding/json"
	"os"
)

type Theme struct {
	Background string `json:"background,omitempty"`
	Foreground string `json:"foreground,omitempty"`
	Borders    bool   `json:"borders,omitempty"`
}

type Config struct {
	GetMessagesLimit uint   `json:"getMessagesLimit,omitempty"`
	Theme            *Theme `json:"theme,omitempty"`
}

func NewConfig() *Config {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	var c Config = Config{
		GetMessagesLimit: 50,
		Theme: &Theme{
			Borders: true,
		},
	}
	configPath := userHomeDir + "/.config/discordo/config.json"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &c
	}

	d, err := os.ReadFile(configPath)
	if err != nil {
		panic(err)
	}

	if err = json.Unmarshal(d, &c); err != nil {
		panic(err)
	}

	return &c
}
