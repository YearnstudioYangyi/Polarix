package parser

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type PositionalParser struct{}

func (p *PositionalParser) Parse(rawMsg string, result any) error {
	// 获取反射值
	val := reflect.ValueOf(result)

	// 必须是指针, 且不为nil
	if val.Kind() != reflect.Pointer || val.IsNil() {
		return fmt.Errorf("result 必须是一个非空的结构体指针")
	}

	// 解引用到具体的结构体对象
	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return fmt.Errorf("result 必须指向一个结构体")
	}

	// 移除命令头
	parts := strings.Fields(rawMsg)
	if len(parts) > 0 {
		parts = parts[1:]
	}

	// 校验参数个数
	if len(parts) < val.NumField() {
		return fmt.Errorf("参数不足，预期 %d 个，实际收到 %d 个", val.NumField(), len(parts))
	}

	// 循环赋值
	for i := 0; i < val.NumField(); i++ {
		fieldVal := val.Field(i)

		// 校验字段是否可被修改
		if !fieldVal.CanSet() {
			return fmt.Errorf("字段 %s 不可写", val.Type().Field(i).Name)
		}

		arg := parts[i]

		switch fieldVal.Kind() {
		case reflect.String:
			fieldVal.SetString(arg)
		case reflect.Int, reflect.Int64:
			parsedInt, err := strconv.ParseInt(arg, 10, 64)
			if err != nil {
				return fmt.Errorf("参数 %d 类型错误: 预期整数", i)
			}
			fieldVal.SetInt(parsedInt)
		case reflect.Bool:
			parsedBool, err := strconv.ParseBool(arg)
			if err != nil {
				return fmt.Errorf("参数 %d 类型错误: 预期布尔值", i)
			}
			fieldVal.SetBool(parsedBool)
		default:
			return fmt.Errorf("不支持的字段类型: %s", fieldVal.Kind())
		}
	}

	return nil
}
