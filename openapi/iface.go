package openapi

import (
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/tidwall/gjson"
)

// OpenAPI is the interface of the openapi
type OpenAPI interface {
	SingleChat(openid string, msg *dto.Message) (gjson.Result, error)      // SingleChat sends a message to the single chat
	GroupChat(groupID string, msg *dto.Message) (gjson.Result, error)      // GroupChat sends a message to the group chat
	SingleRichMedia(openid string, media *dto.Media) (gjson.Result, error) // SingleRichMedia sends a rich media to the single chat
	GroupRichMedia(groupID string, media *dto.Media) (gjson.Result, error) // GroupRichMedia sends a rich media to the group chat
	SingleReset(openid, messageID string) (gjson.Result, error)            // SingleReset resets a message in the single chat
	GroupReset(groupID, messageID string) (gjson.Result, error)            // GroupReset resets a message in the group chat
}
