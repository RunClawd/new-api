package relay

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	"github.com/QuantumNous/new-api/relay/basegate/adapters"
	"github.com/QuantumNous/new-api/relay/channel/task/ali"
	"github.com/QuantumNous/new-api/relay/channel/task/doubao"
	"github.com/QuantumNous/new-api/relay/channel/task/gemini"
	"github.com/QuantumNous/new-api/relay/channel/task/hailuo"
	"github.com/QuantumNous/new-api/relay/channel/task/jimeng"
	"github.com/QuantumNous/new-api/relay/channel/task/kling"
	"github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/suno"
	"github.com/QuantumNous/new-api/relay/channel/task/vertex"
	"github.com/QuantumNous/new-api/relay/channel/task/vidu"
)

// RegisterAllLegacyTaskAdaptors wraps every existing TaskAdaptor in a
// LegacyTaskAdaptorWrapper and registers it in the BaseGate adapter registry.
// This bridges the old task system into the unified /v1/bg/responses pipeline.
//
// Call once during server init (e.g. from main.go or router init).
func RegisterAllLegacyTaskAdaptors() {
	adaptors := []struct {
		name  string
		inner basegate.LegacyTaskBridge
	}{
		{"suno", &suno.TaskAdaptor{}},
		{"ali", &ali.TaskAdaptor{}},
		{"kling", &kling.TaskAdaptor{}},
		{"jimeng", &jimeng.TaskAdaptor{}},
		{"vertex", &vertex.TaskAdaptor{}},
		{"vidu", &vidu.TaskAdaptor{}},
		{"doubao", &doubao.TaskAdaptor{}},
		{"sora", &sora.TaskAdaptor{}},
		{"gemini", &gemini.TaskAdaptor{}},
		{"hailuo", &hailuo.TaskAdaptor{}},
	}

	total := 0
	for _, a := range adaptors {
		wrapper := &basegate.LegacyTaskAdaptorWrapper{
			Inner:        a.inner,
			PlatformName: a.name,
		}
		basegate.RegisterAdapter(wrapper)
		models := a.inner.GetModelList()
		total += len(models)
		common.SysLog("bg_init: registered legacy task adaptor: " + a.name +
			" (" + itoa(len(models)) + " models)")
	}

	common.SysLog("bg_init: total legacy task models registered: " + itoa(total))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 4)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ReloadNativeAdapters clears the registry and re-registers all native adapters from the database.
// Safe to call at runtime (e.g. when channels are added/modified via the admin UI).
func ReloadNativeAdapters() {
	basegate.ClearRegistry()
	RegisterAllLegacyTaskAdaptors()
	RegisterNativeAdapters()
	common.SysLog(fmt.Sprintf("bg_reload: registry reloaded — %d adapters total", basegate.RegisteredAdapterCount()))
}

// RegisterNativeAdapters dynamically initializes Native Adapters from the database.
func RegisterNativeAdapters() {
	// --- OpenAI LLM adapters ---
	var channels []*model.Channel
	if err := model.DB.Where("type = ? AND status = 1", constant.ChannelTypeOpenAI).Find(&channels).Error; err != nil {
		common.SysError("bg_init: failed to load openai channels: " + err.Error())
	} else {
		count := 0
		for _, ch := range channels {
			keys := ch.GetKeys()
			if len(keys) == 0 {
				continue
			}
			apiKey := keys[0]
			adapter := adapters.NewOpenAILLMAdapter(ch.Id, ch.Name, apiKey, ch.GetBaseURL())
			basegate.RegisterAdapter(adapter)
			count++
			common.SysLog(fmt.Sprintf("bg_init: registered native adapter openai_native_ch%d", ch.Id))
		}
		common.SysLog(fmt.Sprintf("bg_init: total OpenAI native adapters registered: %d", count))
	}

	// --- Anthropic LLM adapters ---
	var anthropicChannels []*model.Channel
	if err := model.DB.Where("type = ? AND status = 1", constant.ChannelTypeAnthropic).Find(&anthropicChannels).Error; err != nil {
		common.SysError("bg_init: failed to load anthropic channels: " + err.Error())
	} else {
		count := 0
		for _, ch := range anthropicChannels {
			keys := ch.GetKeys()
			if len(keys) == 0 {
				continue
			}
			adapter := adapters.NewAnthropicLLMAdapter(ch.Id, ch.Name, keys[0], ch.GetBaseURL())
			basegate.RegisterAdapter(adapter)
			count++
			common.SysLog(fmt.Sprintf("bg_init: registered native adapter anthropic_native_ch%d", ch.Id))
		}
		common.SysLog(fmt.Sprintf("bg_init: total Anthropic native adapters registered: %d", count))
	}

	// --- DeepSeek LLM adapters ---
	var deepseekChannels []*model.Channel
	if err := model.DB.Where("type = ? AND status = 1", constant.ChannelTypeDeepSeek).Find(&deepseekChannels).Error; err != nil {
		common.SysError("bg_init: failed to load deepseek channels: " + err.Error())
	} else {
		count := 0
		for _, ch := range deepseekChannels {
			keys := ch.GetKeys()
			if len(keys) == 0 {
				continue
			}
			adapter := adapters.NewDeepSeekLLMAdapter(ch.Id, ch.Name, keys[0], ch.GetBaseURL())
			basegate.RegisterAdapter(adapter)
			count++
			common.SysLog(fmt.Sprintf("bg_init: registered native adapter deepseek_native_ch%d", ch.Id))
		}
		common.SysLog(fmt.Sprintf("bg_init: total DeepSeek native adapters registered: %d", count))
	}

	// --- Gemini LLM adapters ---
	var geminiChannels []*model.Channel
	if err := model.DB.Where("type = ? AND status = 1", constant.ChannelTypeGemini).Find(&geminiChannels).Error; err != nil {
		common.SysError("bg_init: failed to load gemini channels: " + err.Error())
	} else {
		count := 0
		for _, ch := range geminiChannels {
			keys := ch.GetKeys()
			if len(keys) == 0 {
				continue
			}
			adapter := adapters.NewGeminiLLMAdapter(ch.Id, ch.Name, keys[0], ch.GetBaseURL())
			basegate.RegisterAdapter(adapter)
			count++
			common.SysLog(fmt.Sprintf("bg_init: registered native adapter gemini_native_ch%d", ch.Id))
		}
		common.SysLog(fmt.Sprintf("bg_init: total Gemini native adapters registered: %d", count))
	}

	// --- Kling Video adapters ---
	var klingChannels []*model.Channel
	if err := model.DB.Where("type = ? AND status = 1", constant.ChannelTypeKling).Find(&klingChannels).Error; err != nil {
		common.SysError("bg_init: failed to load kling channels: " + err.Error())
	} else {
		klingCount := 0
		for _, ch := range klingChannels {
			keys := ch.GetKeys()
			if len(keys) == 0 {
				continue
			}
			adapter := adapters.NewKlingVideoAdapter(ch.Id, ch.Name, keys[0], ch.GetBaseURL())
			basegate.RegisterAdapter(adapter)
			klingCount++
			common.SysLog(fmt.Sprintf("bg_init: registered native adapter kling_native_ch%d", ch.Id))
		}
		common.SysLog(fmt.Sprintf("bg_init: total Kling native adapters registered: %d", klingCount))
	}

	// --- E2B Sandbox adapters (Channel table first, env var fallback) ---
	var e2bChannels []*model.Channel
	if err := model.DB.Where("type = ? AND status = 1", constant.ChannelTypeE2B).Find(&e2bChannels).Error; err != nil {
		common.SysError("bg_init: failed to load e2b channels: " + err.Error())
	}
	e2bCount := 0
	for _, ch := range e2bChannels {
		keys := ch.GetKeys()
		if len(keys) == 0 {
			continue
		}
		adapter := adapters.NewE2BSandboxAdapter(ch.Id, keys[0], ch.GetBaseURL(), "code-interpreter-v1")
		basegate.RegisterAdapter(adapter)
		e2bCount++
		common.SysLog(fmt.Sprintf("bg_init: registered native adapter e2b_sandbox_ch%d", ch.Id))
	}
	// Fallback to environment variable if no E2B channels configured
	if e2bCount == 0 {
		if e2bKey := common.GetEnvOrDefaultString("E2B_API_KEY", ""); e2bKey != "" {
			adapter := adapters.NewE2BSandboxAdapter(0, e2bKey, "", "code-interpreter-v1")
			basegate.RegisterAdapter(adapter)
			e2bCount++
			common.SysLog("bg_init: registered E2B sandbox adapter from E2B_API_KEY env var")
		}
	}
	common.SysLog(fmt.Sprintf("bg_init: total E2B sandbox adapters registered: %d", e2bCount))

	// --- Dev Sandbox adapter ---
	if common.DebugEnabled {
		sandboxAdapter := &adapters.DummySandboxAdapter{NameID: "dev_sandbox"}
		basegate.RegisterAdapter(sandboxAdapter)
		common.SysLog("bg_init: registered dev sandbox adapter")
	}
}
