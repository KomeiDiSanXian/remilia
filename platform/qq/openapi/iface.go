package openapi

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// OpenAPI is the interface of the openapi
type OpenAPI interface {
	SingleChat(ctx context.Context, openid string, msg *dto.Message) (gjson.Result, error)          // SingleChat sends a message to the single chat
	GroupChat(ctx context.Context, groupID string, msg *dto.Message) (gjson.Result, error)          // GroupChat sends a message to the group chat
	ChannelChat(ctx context.Context, channelID string, msg *dto.GuildMessage) (gjson.Result, error) // ChannelChat sends a message to a guild text channel
	SingleRichMedia(ctx context.Context, openid string, media *dto.Media) (gjson.Result, error)     // SingleRichMedia sends a rich media to the single chat
	GroupRichMedia(ctx context.Context, groupID string, media *dto.Media) (gjson.Result, error)     // GroupRichMedia sends a rich media to the group chat
	SingleReset(ctx context.Context, openid, messageID string) (gjson.Result, error)                // SingleReset resets a message in the single chat
	GroupReset(ctx context.Context, groupID, messageID string) (gjson.Result, error)                // GroupReset resets a message in the group chat
}
