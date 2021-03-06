package pluggable

import (
	"testing"

	"github.com/deislabs/porter/pkg/config"
	"github.com/deislabs/porter/pkg/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginLoader_SelectPlugin(t *testing.T) {
	c := config.NewTestConfig(t)
	l := NewPluginLoader(c.Config)

	pluginCfg := PluginTypeConfig{
		GetDefaultPluggable: func(datastore *config.Data) string {
			return datastore.GetDefaultInstanceStore()
		},
		GetPluggable: func(datastore *config.Data, name string) (Entry, error) {
			return datastore.GetInstanceStore(name)
		},
		GetDefaultPlugin: func(datastore *config.Data) string {
			return datastore.GetInstanceStoragePlugin()
		},
	}

	t.Run("internal plugin", func(t *testing.T) {
		c.Data = &config.Data{
			InstanceStoragePlugin: "filesystem",
		}

		err := l.selectPlugin(pluginCfg)
		require.NoError(t, err, "error selecting plugin")

		assert.Equal(t, &plugins.PluginKey{Binary: "porter", Implementation: "filesystem", IsInternal: true}, l.SelectedPluginKey)
		assert.Nil(t, l.SelectedPluginConfig)
	})

	t.Run("external plugin", func(t *testing.T) {
		c.Data = &config.Data{
			InstanceStoragePlugin: "azure.blob",
		}

		err := l.selectPlugin(pluginCfg)
		require.NoError(t, err, "error selecting plugin")

		assert.Equal(t, &plugins.PluginKey{Binary: "azure", Implementation: "blob", IsInternal: false}, l.SelectedPluginKey)
		assert.Nil(t, l.SelectedPluginConfig)
	})

	t.Run("configured plugin", func(t *testing.T) {
		c.Data = &config.Data{
			DefaultInstanceStore: "azure",
			InstanceStores: []config.InstanceStore{
				{
					Name:         "azure",
					PluginSubKey: "azure.blob",
					Config: map[string]interface{}{
						"env": "MyAzureConnString",
					},
				},
			},
		}

		err := l.selectPlugin(pluginCfg)
		require.NoError(t, err, "error selecting plugin")

		assert.Equal(t, &plugins.PluginKey{Binary: "azure", Implementation: "blob", IsInternal: false}, l.SelectedPluginKey)
		assert.Equal(t, c.Data.InstanceStores[0].Config, l.SelectedPluginConfig)
	})
}
