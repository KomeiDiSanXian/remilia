package constant

const (
	// AccessTokenURL is the url to get access token
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html#%E8%8E%B7%E5%8F%96%E8%B0%83%E7%94%A8%E5%87%AD%E8%AF%81
	AccessTokenURL = "https://bots.qq.com/app/getAppAccessToken"
	// OpenAPIURL is the base url of qq bot openapi
	//
	// should fill authorization header with access token like: QQBot {access_token}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html#%E9%89%B4%E6%9D%83%E6%96%B9%E5%BC%8F
	OpenAPIURL = "https://api.sgroup.qq.com"
	// GatewayURL is the url to get websocket gateway
	//
	// must fill Content-Type with application/json
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/openapi/wss/url_get.html
	GatewayURL = OpenAPIURL + "/gateway"

	// SingleChatURL /v2/users/{openid}/messages
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html#%E5%8D%95%E8%81%8A
	SingleChatURL = OpenAPIURL + "/v2/users/%s/messages"

	// GroupChatURL /v2/groups/{group_openid}/messages
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html#%E7%BE%A4%E8%81%8A
	GroupChatURL = OpenAPIURL + "/v2/groups/%s/messages"

	// SingleRichMediaURL /v2/users/{openid}/files
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html#%E7%94%A8%E4%BA%8E%E5%8D%95%E8%81%8A
	SingleRichMediaURL = OpenAPIURL + "/v2/users/%s/files"

	// GroupRichMediaURL /v2/groups/{group_openid}/files
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html#%E7%94%A8%E4%BA%8E%E7%BE%A4%E8%81%8A
	GroupRichMediaURL = OpenAPIURL + "/v2/groups/%s/files"

	// SingleResetURL /v2/users/{openid}/messages/{msg_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E5%8D%95%E8%81%8A
	SingleResetURL = OpenAPIURL + "/v2/users/%s/messages/%s"

	// GroupResetURL /v2/groups/{group_openid}/messages/{msg_id}
	//
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/reset.html#%E7%BE%A4%E8%81%8A
	GroupResetURL = OpenAPIURL + "/v2/groups/%s/messages/%s"
)
