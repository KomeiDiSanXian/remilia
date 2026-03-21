// Package openapi is qq bot openapi
package openapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/KomeiDiSanXian/remilia/infra/httpclient"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/constant"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// Client 对 OpenAPI 的实现
type Client struct {
	tm *token.Manager
}

// New 创建 OpenAPI 服务
func New(manager *token.Manager) *Client {
	return &Client{tm: manager}
}

// Post 发送一个 post 请求到 url，自动添加 Authorization 头，ctx 用于传播超时/取消。
func (api *Client) Post(ctx context.Context, url string, data any) (gjson.Result, error) {
	api.tm.WaitReady()
	result, err := httpclient.Post(url).
		SetContext(ctx).
		SetHeader("Authorization", fmt.Sprintf("QQBot %s", api.tm.GetToken())).
		SetJSON(data).
		DoJSON()
	if err != nil {
		logger.WithError(err).WithField("url", url).Error("[OpenAPI] Post failed")
		return gjson.Result{}, err
	}
	return result, nil
}

// Delete 发送一个 delete 请求到 url，自动添加 Authorization 头，ctx 用于传播超时/取消。
func (api *Client) Delete(ctx context.Context, url string) (gjson.Result, error) {
	api.tm.WaitReady()
	resp, err := httpclient.Delete(url).
		SetContext(ctx).
		SetHeader("Authorization", fmt.Sprintf("QQBot %s", api.tm.GetToken())).
		SetHeader("Content-Type", "application/json").
		Do()
	if err != nil {
		logger.WithError(err).WithField("url", url).Error("[OpenAPI] Delete failed")
		return gjson.Result{}, err
	}
	defer resp.Close()
	if resp.StatusCode != http.StatusOK {
		logger.WithField("status", resp.Status).WithField("url", url).Error("[OpenAPI] Delete failed")
		return gjson.Result{}, fmt.Errorf("status code not 200: %s", resp.Status)
	}
	return resp.JSON()
}

func (api *Client) SingleChat(ctx context.Context, openid string, msg *dto.Message) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.SingleChatURL, openid), msg)
}

func (api *Client) GroupChat(ctx context.Context, groupOpenid string, msg *dto.Message) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupChatURL, groupOpenid), msg)
}

// ChannelChat 向文字子频道发送消息。
//
// channelID 为子频道 ID（ChatInfo.ID），消息格式由 msg 中的非空字段推断：
// Markdown > Content（文本）> Image（图片）。
// 至少需要填充 Content、Embed、Ark、Image 或 Markdown 中的一个字段。
func (api *Client) ChannelChat(ctx context.Context, channelID string, msg *dto.GuildMessage) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.ChannelChatURL, channelID), msg)
}

func (api *Client) SingleRichMedia(ctx context.Context, openid string, media *dto.Media) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.SingleRichMediaURL, openid), media)
}

func (api *Client) GroupRichMedia(ctx context.Context, groupOpenid string, media *dto.Media) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupRichMediaURL, groupOpenid), media)
}

// SingleReset resets a message in a single chat
func (api *Client) SingleReset(ctx context.Context, openid, messageID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.SingleResetURL, openid, messageID))
}

// GroupReset resets a message in a group chat
func (api *Client) GroupReset(ctx context.Context, groupOpenid, messageID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.GroupResetURL, groupOpenid, messageID))
}
