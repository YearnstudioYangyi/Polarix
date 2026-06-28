package models

// 用户权限组
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
