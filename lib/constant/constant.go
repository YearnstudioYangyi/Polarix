package constant

import (
	"fmt"
	"strings"
)

// UserRole 定义自定义类型
type UserRole string

const (
	RoleOwner  UserRole = "owner"
	RoleAdmin  UserRole = "admin"
	RoleMember UserRole = "member"
)

// 传入的用户权限是否大于等于需要的权限
func (require UserRole) CanUse(user UserRole) bool {
	switch require {
	case RoleOwner:
		return user == RoleOwner
	case RoleAdmin:
		return user == RoleAdmin || user == RoleOwner
	default:
		return true
	}
}

// UnmarshalJSON 实现 Unmarshaler 接口以进行校验
func (r *UserRole) UnmarshalJSON(b []byte) error {
	// 去掉 JSON 字符串前后的引号
	s := strings.Trim(string(b), "\"")

	role := UserRole(s)
	switch role {
	case RoleAdmin, RoleMember, RoleOwner:
		*r = role
		return nil
	default:
		return fmt.Errorf("invalid UserRole: %s", s)
	}
}

// User 结构体定义
type User struct {
	Name string   `json:"name"`
	Role UserRole `json:"role"`
}
