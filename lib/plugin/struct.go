package plugin

import (
	"Plrx/lib/constant"
	"Plrx/lib/context"
	"Plrx/lib/parser"
	"reflect"
)

// 模板函数
type HandleFunc func(*context.Context) error

type Command struct {
	Prefix       string
	Role         constant.UserRole
	Describle    string
	Handle       HandleFunc
	PluginId     string
	Parser       parser.Parser
	ParserTarget reflect.Type
}

type PluginConfig struct {
	Id       string
	Commands []*Command
}
