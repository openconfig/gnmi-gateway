package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_pathMetadata(t *testing.T) {
	assertion := assert.New(t)

	cfg := NewDefaultGatewayConfig()

	cfg.AddPathMetadata("/test/a", map[string]string{
		"a": "childA",
		"b": "childB",
	})
	cfg.AddPathMetadata("/test", map[string]string{
		"a": "parentA",
		"c": "parentC",
	})
	cfg.AddPathMetadata("/", map[string]string{
		"d": "root",
	})

	assertion.Equal(
		map[string]string{
			"a": "childA",
			"b": "childB",
			"c": "parentC",
			"d": "root",
		},
		cfg.GetPathMetadata("/test/a"),
	)
	assertion.Equal(
		map[string]string{
			"a": "parentA",
			"c": "parentC",
			"d": "root",
		},
		cfg.GetPathMetadata("/test"),
	)
	assertion.Equal(
		map[string]string{
			"d": "root",
		},
		cfg.GetPathMetadata("/"),
	)
	assertion.Equal(
		map[string]string{
			"d": "root",
		},
		cfg.GetPathMetadata("/some/other/path"),
	)
}
