package context

import (
	"Plrx/lib/constant"
	"Plrx/lib/qqapi"
	"Plrx/lib/requests"
	"Plrx/lib/storage"
	"fmt"
	"time"
)

type Context struct {
	*MessageManager
	PluginID       string
	Request        *requests.Client
	GlobalStorage  *storage.Store
	PluginStorage  *storage.Store
	CommandStorage *storage.Store
	UserStorage    *storage.Store
	GroupStorage   *storage.Store
}

// 初始化Context对象及MessageManager对象
func (context *Context) Init(messageId, eventId string, qqapi *qqapi.Client) {
	context.MessageManager = &MessageManager{
		MessageId: messageId,
		EventId:   eventId,
		Qapi:      qqapi,
	}
	context.Request = qqapi.Request
	context.GlobalStorage = storage.Global()
}

// BindStorage exposes namespaces bound to the command currently being handled.
func (context *Context) BindStorage(pluginID, commandID string) {
	context.PluginID = pluginID
	context.PluginStorage = storage.Plugin(pluginID)
	context.CommandStorage = storage.Command(pluginID, commandID)
}

func (context *Context) SetGroupId(id string) {
	context.MessageManager.GroupId = id
	if id != "" {
		context.GroupStorage = storage.Group(id)
	}
}

func (context *Context) SetUserId(id string) {
	context.MessageManager.UserId = id
	if id != "" {
		context.UserStorage = storage.User(id)
	}
}

func (context *Context) SetMessageOrigin(origin constant.MessageOrigin) {
	context.MessageManager.Target = origin
}

// Ban bans the current context user for duration. Existing mutes are removed first.
func (context *Context) Ban(duration time.Duration) error {
	if context.MessageManager == nil || context.UserId == "" {
		return fmt.Errorf("ban member: user ID is not available in this context")
	}
	return context.BanUser(context.UserId, duration)
}

// BanUser bans a specific group member for duration. Existing mutes are removed first.
func (context *Context) BanUser(memberOpenID string, duration time.Duration) error {
	if context.MessageManager == nil || context.GroupId == "" {
		return fmt.Errorf("ban member: group ID is not available in this context")
	}
	if context.Qapi == nil {
		return fmt.Errorf("ban member: QQ API client is not available")
	}
	if memberOpenID == "" {
		return fmt.Errorf("ban member: member OpenID is required")
	}
	if duration <= 0 || duration > 30*24*time.Hour {
		return fmt.Errorf("ban member: duration must be greater than 0 and no more than 30 days")
	}
	if err := context.Qapi.UnbanGroupMember(context.GroupId, memberOpenID); err != nil {
		return err
	}
	return context.Qapi.BanGroupMember(context.GroupId, memberOpenID, time.Now().Add(duration).Format(time.RFC3339))
}

// Unban immediately removes the current context user's mute.
func (context *Context) Unban() error {
	if context.MessageManager == nil || context.UserId == "" {
		return fmt.Errorf("unban member: user ID is not available in this context")
	}
	return context.UnbanUser(context.UserId)
}

// UnbanUser immediately removes a specific group member's mute.
func (context *Context) UnbanUser(memberOpenID string) error {
	if context.MessageManager == nil || context.GroupId == "" {
		return fmt.Errorf("unban member: group ID is not available in this context")
	}
	if context.Qapi == nil {
		return fmt.Errorf("unban member: QQ API client is not available")
	}
	if memberOpenID == "" {
		return fmt.Errorf("unban member: member OpenID is required")
	}
	return context.Qapi.UnbanGroupMember(context.GroupId, memberOpenID)
}
