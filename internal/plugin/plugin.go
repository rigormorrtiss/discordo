package plugin

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/ayn2op/discordo/internal/constants"
	extism "github.com/extism/go-sdk"
)

var plugins []*extism.Plugin

func Load() error {
	path := filepath.Join(constants.ConfigDirPath, "plugins")
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return err
	}

	ctx := context.TODO()
	config := extism.PluginConfig{}
	for _, entry := range entries {
		manifest := extism.Manifest{
			Wasm: []extism.Wasm{
				extism.WasmFile{
					Name: entry.Name(),
					Path: filepath.Join(path, entry.Name()),
				},
			},
		}
		plugin, err := extism.NewPlugin(ctx, manifest, config, nil)
		if err != nil {
			log.Println(err)
			continue
		}

		plugins = append(plugins, plugin)
	}

	return nil
}
