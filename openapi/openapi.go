// Package openapi is qq bot openapi
package openapi

import (
	"fmt"
	"io"
	"net/http"

	"github.com/KomeiDiSanXian/remilia/httpreq"
	"github.com/KomeiDiSanXian/remilia/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/openapi/constant"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// Service 对 OpenAPI 的实现
type Service struct {
	tm *token.Manager
}

// New 创建 OpenAPI 服务
func New(manager *token.Manager) *Service {
	return &Service{
		tm: manager,
	}
}

// Post 发送一个 post 请求到 url
//
// 会自动添加 Authorization 头
func (api *Service) Post(url string, data any) (gjson.Result, error) {
	api.tm.WaitReady()
	result, err := httpreq.NewPost(url).
		SetHeader("Authorization", fmt.Sprintf("QQBot %s", api.tm.GetToken())).
		SetJSONBody(data).
		DoJSON()
	if err != nil {
		logrus.WithError(err).WithField("url", url).Error("[OpenAPI] Post failed")
		return gjson.Result{}, err
	}
	return result, nil
}

// Delete 发送一个 delete 请求到 url
//
// 会自动添加 Authorization 头
func (api *Service) Delete(url string) (gjson.Result, error) {
	api.tm.WaitReady()
	resp, err := httpreq.New(url, http.MethodDelete).
		SetHeader("Authorization", fmt.Sprintf("QQBot %s", api.tm.GetToken())).
		SetHeader("Content-Type", "application/json").
		Do()
	if err != nil {
		logrus.WithError(err).WithField("url", url).Error("[OpenAPI] Delete failed")
		return gjson.Result{}, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logrus.WithField("status", resp.Status).WithField("url", url).Error("[OpenAPI] Delete failed")
		return gjson.Result{}, fmt.Errorf("status code not 200: %s", resp.Status)
	}
	return httpreq.ParseJSON(resp.Body)
}

// SingleChat sends a message to a single chat
//
// openid can be got from the "payload.detail"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html#%E5%8D%95%E8%81%8A
func (api *Service) SingleChat(openid string, msg *dto.Message) (gjson.Result, error) {
	return api.Post(fmt.Sprintf(constant.SingleChatURL, openid), msg)
}

// GroupChat sends a message to a group chat
//
// group_openid can be got from the "payload.detail"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html#%E7%BE%A4%E8%81%8A
func (api *Service) GroupChat(groupOpenid string, msg *dto.Message) (gjson.Result, error) {
	return api.Post(fmt.Sprintf(constant.GroupChatURL, groupOpenid), msg)
}

// SingleRichMedia sends a rich media to a single chat
//
// openid can be got from the "payload.detail"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html#%E7%94%A8%E4%BA%8E%E5%8D%95%E8%81%8A
func (api *Service) SingleRichMedia(openid string, media *dto.Media) (gjson.Result, error) {
	return api.Post(fmt.Sprintf(constant.SingleRichMediaURL, openid), media)
}

// GroupRichMedia sends a rich media to a group chat
//
// group_openid can be got from the "payload.detail"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html#%E7%94%A8%E4%BA%8E%E7%BE%A4%E8%81%8A
func (api *Service) GroupRichMedia(groupOpenid string, media *dto.Media) (gjson.Result, error) {
	return api.Post(fmt.Sprintf(constant.GroupRichMediaURL, groupOpenid), media)
}

// SingleReset resets a message in a single chat
//
// openid can be got from the "payload.detail"
//
// message_id can be got from the "payload.detail"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E5%8D%95%E8%81%8A
func (api *Service) SingleReset(openid, messageID string) (gjson.Result, error) {
	return api.Delete(fmt.Sprintf(constant.SingleResetURL, openid, messageID))
}

// GroupReset resets a message in a group chat
//
// group_openid can be got from the "payload.detail"
//
// message_id can be got from the "payload.detail"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E7%BE%A4%E8%81%8A
func (api *Service) GroupReset(groupOpenid, messageID string) (gjson.Result, error) {
	return api.Delete(fmt.Sprintf(constant.GroupResetURL, groupOpenid, messageID))
}
