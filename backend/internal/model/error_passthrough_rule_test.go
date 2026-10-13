package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllPlatformsIncludesEveryConcretePlatform(t *testing.T) {
	require.ElementsMatch(t, []string{
		"anthropic",
		"openai",
		"gemini",
		"antigravity",
		"grok",
		"kimi",
		"zhipu",
		"deepseek",
		"minimax",
		"opencode_go",
		"typesafe",
		"command_code",
		"cline",
		// 二开源码集成三平台。
		"workbuddy",
		"qoder",
		"trae",
	}, AllPlatforms())
}
