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

// 处理新成员入群
type memberAddHandle func(*context.GroupMemberAddContext) error

var memberAddHandleFunc memberAddHandle = nil
var memberAddHandleLock *sync.Mutex = &sync.Mutex{}

func SetGlobalMemberAddHandle(handle memberAddHandle) {
	memberAddHandleLock.Lock()
	defer memberAddHandleLock.Unlock()
	memberAddHandleFunc = handle
}

func CallGlobalMemberAddHandle(ctx *context.GroupMemberAddContext) error {
	memberAddHandleLock.Lock()
	defer memberAddHandleLock.Unlock()
	if memberAddHandleFunc == nil {
		return fmt.Errorf("Didn't set group member add handle function")
	}
	return memberAddHandleFunc(ctx)
}
