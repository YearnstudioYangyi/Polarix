package buttons

import (
	"Plrx/lib/context"
	"fmt"
	"strconv"
	"sync"
)

type CallbackInfo struct {
	Handle CallbackButtonHandleFunc
	Id     string
}

// 最大存储上限
var MaxCallbackStorage uint = 10000

var CallbackMap []*CallbackInfo = make([]*CallbackInfo, MaxCallbackStorage)

// 起始位置
var startIndex uint = 0

// 当前空位
var nowIndex uint = 0

// 同步锁
var lock *sync.Mutex = &sync.Mutex{}

// 保存映射关系并生成真实ID
func RegisterCallbackFunc(id string, handle CallbackButtonHandleFunc) string {
	lock.Lock()
	defer lock.Unlock()
	realId := nowIndex
	CallbackMap[realId] = &CallbackInfo{
		Id:     id,
		Handle: handle,
	}
	nowIndex++
	if nowIndex > MaxCallbackStorage-1 {
		// 溢出, 从头开始覆盖
		nowIndex = 0
	}
	if nowIndex == startIndex {
		// 当前位置 与 起始位置重合, 让起始位置后推, 废弃掉前面的数据
		startIndex++
		if startIndex > MaxCallbackStorage-1 {
			// 循环
			startIndex = 0
		}
	}
	return strconv.Itoa(int(realId))
}

func InvokeCallback(id string, ctx *context.CallbackContext) error {
	// 获得读锁
	lock.Lock()
	targetId, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("Trans id into number failed: %v", err)
	}
	// 不可能出现非数字, 直接忽略错误
	startId, _ := strconv.Atoi(CallbackMap[startIndex].Id)
	// 计算真实索引
	realIndex := (targetId - startId + int(startIndex)) % int(MaxCallbackStorage)
	// 获取数据
	handle := CallbackMap[realIndex].Handle
	buttonId := CallbackMap[realIndex].Id
	// 释放锁, 防止回调函数内注册新的按钮导致的死锁
	lock.Unlock()
	// 判空
	if handle == nil {
		return fmt.Errorf("Callback button: %v did not register", id)
	}
	ctx.ButtonId = buttonId
	return handle(ctx)
}
