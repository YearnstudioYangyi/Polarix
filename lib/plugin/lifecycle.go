package plugin

import (
	"Plrx/lib/context"
	"fmt"
	"sync"
)

// 处理入群请求
type joinGroupHandle func(*context.ApplyJoinGroupContext) error

var joinGroupHandleFunc joinGroupHandle
var joinGroupPluginID string
var joinGroupHandleLock sync.Locker = &sync.Mutex{}

func SetGlobalJoinGroupHandle(handle joinGroupHandle) {
	setGlobalJoinGroupHandle("", handle)
}

func setGlobalJoinGroupHandle(pluginID string, handle joinGroupHandle) {
	joinGroupHandleLock.Lock()
	defer joinGroupHandleLock.Unlock()
	joinGroupHandleFunc = handle
	joinGroupPluginID = pluginID
}

func CallGlobalJoinGroupHandle(ctx *context.ApplyJoinGroupContext) error {
	joinGroupHandleLock.Lock()
	defer joinGroupHandleLock.Unlock()
	if joinGroupHandleFunc == nil {
		return fmt.Errorf("Didn't set join group handle function")
	}
	if joinGroupPluginID != "" {
		ctx.BindStorage(joinGroupPluginID, "join-group")
	}
	return joinGroupHandleFunc(ctx)
}
