package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/basegate"
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
