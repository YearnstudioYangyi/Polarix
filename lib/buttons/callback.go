package buttons

import "sync"

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
var lock *sync.RWMutex = &sync.RWMutex{}

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

	}
}
