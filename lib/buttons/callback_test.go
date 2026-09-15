package buttons_test

import (
	"fmt"
	"sync"
	"testing"

	// 假设你的项目路径如下，正常引入即可
	"Plrx/lib/buttons"
	"Plrx/lib/context"
)

// TestBasicRegisterAndInvoke 测试最基础的注册和触发流程
func TestBasicRegisterAndInvoke(t *testing.T) {
	internalID := "test_btn_001"
	isExecuted := false

	// 1. 注册回调
	extID := buttons.RegisterCallbackFunc(internalID, func(ctx *context.CallbackContext) error {
		isExecuted = true
		return nil
	})

	if extID == "" {
		t.Fatal("预期返回有效的外部ID，但得到了空字符串")
	}

	// 2. 准备上下文并触发回调
	ctx := &context.CallbackContext{}
	err := buttons.InvokeCallback(extID, ctx)

	// 3. 断言校验结果
	if err != nil {
		t.Fatalf("触发回调失败: %v", err)
	}
	if !isExecuted {
		t.Fatal("回调函数未被执行")
	}
	if ctx.ButtonId != internalID {
		t.Fatalf("上下文的 ButtonId 不匹配，预期: %s, 实际: %s", internalID, ctx.ButtonId)
	}
}

// TestMultipleCallbacks 测试多个按钮注册时，数据是否会串话（互相干扰）
func TestMultipleCallbacks(t *testing.T) {
	var executedA, executedB bool

	buttons.RegisterCallbackFunc("btn_A", func(ctx *context.CallbackContext) error {
		executedA = true
		return nil
	})

	extIdB := buttons.RegisterCallbackFunc("btn_B", func(ctx *context.CallbackContext) error {
		executedB = true
		return nil
	})

	// 触发 B
	ctxB := &context.CallbackContext{}
	err := buttons.InvokeCallback(extIdB, ctxB)
	if err != nil {
		t.Fatalf("触发B失败: %v", err)
	}

	// B执行了，A没执行，且ButtonId正确
	if !executedB || executedA {
		t.Fatal("回调执行错乱，发生串话")
	}
	if ctxB.ButtonId != "btn_B" {
		t.Fatalf("期望 ButtonId 为 btn_B，实际为 %s", ctxB.ButtonId)
	}
}

// TestRingBufferOverflow 测试超过 MaxCallbackStorage 时的溢出覆盖情况
func TestRingBufferOverflow(t *testing.T) {
	// 获取容量上限
	maxCap := int(buttons.MaxCallbackStorage)

	firstInternalID := "first_btn"
	firstExtID := buttons.RegisterCallbackFunc(firstInternalID, func(ctx *context.CallbackContext) error {
		return fmt.Errorf("最早的数据，不应该被执行")
	})

	// 塞入数据，直到超过容量上限，刚好把第一个数据挤掉
	for i := range maxCap {
		buttons.RegisterCallbackFunc(fmt.Sprintf("fill_btn_%d", i), func(ctx *context.CallbackContext) error {
			return nil
		})
	}

	// 此时再尝试调用第一个ID，由于黑盒测试不知道内部实现细节，
	// 但合理的表现应该是：要么报错找不到，要么执行的不是原来那个函数。
	// 这里重点保证不会 panic 崩溃即可。
	ctx := &context.CallbackContext{}
	err := buttons.InvokeCallback(firstExtID, ctx)

	// 在你的实现中，由于没有校验原始 ID 的比对，大概率会静默执行了新覆盖上去的函数。
	// 但无论如何，程序不应该 Panic。
	t.Logf("尝试调用被覆盖的早期ID结果: err=%v, btnId=%s", err, ctx.ButtonId)
}

// TestConcurrentRegistration 测试高并发下的注册，确保锁机制生效且不发生 Data Race
func TestConcurrentRegistration(t *testing.T) {
	var wg sync.WaitGroup
	goroutineCount := 1000

	wg.Add(goroutineCount)
	for i := range goroutineCount {
		go func(idx int) {
			defer wg.Done()
			internalID := fmt.Sprintf("concurrent_btn_%d", idx)
			extID := buttons.RegisterCallbackFunc(internalID, func(ctx *context.CallbackContext) error {
				return nil
			})

			if extID == "" {
				t.Errorf("并发注册生成了空的外部ID")
			}
		}(i)
	}

	// 等待所有 goroutine 完成，如果没有死锁且不报 panic，并发测试通过
	wg.Wait()
}

// TestInvalidExternalID 测试传入非法的外部ID
func TestInvalidExternalID(t *testing.T) {
	ctx := &context.CallbackContext{}
	// 传入非法字符，应当被 strconv.Atoi 拦截并返回 error
	err := buttons.InvokeCallback("not_a_number", ctx)
	if err == nil {
		t.Fatal("传入非数字的外部ID，预期应当返回错误，但返回了 nil")
	}
}
