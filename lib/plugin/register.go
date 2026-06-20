package plugin

import (
	"botOffical/lib/parser"
	"sync"
)

// 所有已注册的命令
var (
	GlobalCommands = make(map[string]*Command)
	mu             sync.RWMutex
)

// 插件调用此函数进行注册
func Register(plugin *PluginConfig) {
	mu.Lock()
	defer mu.Unlock()
	for _, v := range plugin.Commands {
		v.PluginId = plugin.Id
		if v.Parser == nil {
			v.Parser = &parser.DefaultParser{} // 如果没有自定义解析器, 使用默认的解析器, 返回原始字段
		}
		GlobalCommands[v.Prefix] = v
	}
}

// GetCommand 查找命令
func GetCommand(name string) (*Command, bool) {
	mu.RLock()
	defer mu.RUnlock()
	cmd, ok := GlobalCommands[name]
	return cmd, ok
}
