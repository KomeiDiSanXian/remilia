package dto

// ────────────────────────────────────────────────────────────────────────────
// 全局自定义菜单（/v2/menu）
//
// 自定义菜单仅支持 C2C（单聊）场景，设置后对所有用户生效，不支持按用户维度区分。
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_menu.put.html
// ────────────────────────────────────────────────────────────────────────────

// Menu 全局自定义菜单配置。
type Menu struct {
	// Items 菜单项列表，最多 10 个，按列表顺序从左到右展示
	Items []MenuItem `json:"items,omitempty"`
}

// MenuItem 自定义菜单项。
type MenuItem struct {
	// Name 按钮名称，最多 10 个字符，一个中文汉字算 2 个字符
	Name string `json:"name,omitempty"`
	// Type 按钮类型：switch（开关）、send_message（发送消息）、link（链接跳转）、menu（含子菜单的折叠项）
	Type string `json:"type,omitempty"`
	// SubMenuItems 子菜单列表，仅 type=menu 时有效。子菜单最多 5 个，不支持再嵌套子菜单
	SubMenuItems []SubMenuItem `json:"sub_menu_items,omitempty"`
	// SendMessage 发送的内容，仅 type=send_message 时有效。用户点击后该文本会自动填入聊天输入框
	SendMessage string `json:"send_message,omitempty"`
	// Link 跳转链接 URL，仅 type=link 时有效，必须以 https:// 开头
	Link string `json:"link,omitempty"`
	// Switch 开关配置，仅 type=switch 时有效
	Switch *Switch `json:"switch,omitempty"`
}

// SubMenuItem 自定义菜单二级子菜单项。
type SubMenuItem struct {
	// Name 按钮名称，最多 14 个字符（约 7 个中文汉字）
	Name string `json:"name,omitempty"`
	// Type 按钮类型：send_message（发送消息）、link（链接跳转）。二级菜单不支持 menu 类型
	Type string `json:"type,omitempty"`
	// SendMessage 发送的内容，仅 type=send_message 时有效
	SendMessage string `json:"send_message,omitempty"`
	// Link 跳转链接 URL，仅 type=link 时有效，必须以 https:// 开头
	Link string `json:"link,omitempty"`
}

// Switch 自定义菜单开关配置。
type Switch struct {
	// SwitchID 开关唯一标识。用户切换开关状态后会发送一条消息，
	// 消息内容中会携带此字段（如 switch_id 为 "search" 时，打开后
	// 消息的 ext 字段中会携带 "search=1" 的标识，关闭后不携带）
	SwitchID string `json:"switch_id,omitempty"`
	// Default 开关的初始状态。true 表示默认打开，false 表示默认关闭
	Default bool `json:"default,omitempty"`
}

// UpdateMenuRequest 修改全局自定义菜单请求体（PUT /v2/menu）。
type UpdateMenuRequest struct {
	// Menu 菜单配置。传入后会覆盖原有的完整菜单配置
	Menu *Menu `json:"menu,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// 指令面板（/v2/panels）
//
// 支持 c2c（单聊）、group（群聊）、channel（文字子频道）、dm（频道私信）四种场景。
// 其中 c2c 和 group 场景支持按指定用户/群生效（target_type=specific），
// channel 和 dm 场景仅支持全局配置（target_type=all）。一个机器人最多创建 20 个面板。
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_panels.post.html
// ────────────────────────────────────────────────────────────────────────────

// CreatePanelRequest 创建指令面板请求体（POST /v2/panels）。
type CreatePanelRequest struct {
	// Scope 生效场景：c2c（单聊）、group（群聊）、channel（文字子频道）、dm（频道私信）
	Scope string `json:"scope"`
	// TargetType 作用范围：all（对该场景下所有用户/群生效）、specific（仅对指定用户/群生效）。
	// 仅 c2c 和 group 场景支持 specific；channel 和 dm 场景只能传 all
	TargetType string `json:"target_type,omitempty"`
	// UserOpenIDs 用户 openid 列表，仅 c2c 场景且 target_type=specific 时有效，一次最多 20 个
	UserOpenIDs []string `json:"user_openids,omitempty"`
	// GroupOpenIDs 群 openid 列表，仅 group 场景且 target_type=specific 时有效，一次最多 20 个
	GroupOpenIDs []string `json:"group_openids,omitempty"`
	// Panel 面板配置内容
	Panel *Panel `json:"panel"`
}

// Panel 指令面板配置内容。
type Panel struct {
	// Items 面板元素列表，一个指令面板里最多配置 20 个面板元素
	Items []PanelItem `json:"items,omitempty"`
	// Remark 面板备注，用于开发者标记面板用途，最多 255 个字符，不对用户展示
	Remark string `json:"remark,omitempty"`
	// Version 当前版本号
	Version int `json:"version,omitempty"`
}

// PanelItem 指令面板元素。
type PanelItem struct {
	// Name 元素名称。type=command 时用户点击后该内容会填入聊天输入框；
	// type=link 时仅用于面板展示。最多 14 个字符（约 7 个中文汉字）
	Name string `json:"name,omitempty"`
	// Desc 元素描述，用于补充说明该指令或链接的功能，在面板中展示给用户。
	// 最多 30 个字符（约 15 个中文汉字）
	Desc string `json:"desc,omitempty"`
	// Type 元素类型：command（指令）、link（链接跳转）
	Type string `json:"type,omitempty"`
	// OnlyAdmin 是否仅管理员可操作。true 时仅频道/群管理员可点击
	OnlyAdmin bool `json:"only_admin,omitempty"`
	// Link 跳转链接 URL，仅 type=link 时有效
	Link string `json:"link,omitempty"`
}

// PanelRecord 指令面板记录（列表/详情响应中的面板实体）。
type PanelRecord struct {
	// PanelID 面板 ID
	PanelID string `json:"panel_id,omitempty"`
	// Scope 生效场景：c2c、group、channel、dm
	Scope string `json:"scope,omitempty"`
	// TargetType 作用范围：all（全局配置）、specific（指定用户/群生效）
	TargetType string `json:"target_type,omitempty"`
	// Panel 面板配置内容
	Panel *Panel `json:"panel,omitempty"`
	// CreatedAt 面板创建时间，RFC3339 格式
	CreatedAt string `json:"created_at,omitempty"`
	// UpdatedAt 面板更新时间，RFC3339 格式
	UpdatedAt string `json:"updated_at,omitempty"`
	// Version 面板版本号
	Version int `json:"version,omitempty"`
	// UserOpenIDs 关联的用户 openid 列表。仅 c2c 场景且 target_type=specific 时返回，最多 1000 条
	UserOpenIDs []string `json:"user_openids,omitempty"`
	// GroupOpenIDs 关联的群 openid 列表。仅 group 场景且 target_type=specific 时返回，最多 1000 条
	GroupOpenIDs []string `json:"group_openids,omitempty"`
}

// UpdatePanelRequest 修改指令面板请求体（PUT /v2/panels/{panel_id}）。
type UpdatePanelRequest struct {
	// Panel 面板配置内容
	Panel *Panel `json:"panel,omitempty"`
}

// UpdatePanelTargetRequest 修改指令面板关联对象请求体（PUT /v2/panels/{panel_id}/target）。
//
// c2c 场景操作用户 openid，group 场景操作群 openid；channel 和 dm 场景为全局配置，不支持此操作。
type UpdatePanelTargetRequest struct {
	// Op 操作类型：add（添加关联对象）、del（移除关联对象）
	Op string `json:"op"`
	// UserOpenIDs 用户 openid 列表，仅 c2c 场景有效，一次最多 20 个
	UserOpenIDs []string `json:"user_openids,omitempty"`
	// GroupOpenIDs 群 openid 列表，仅 group 场景有效，一次最多 20 个
	GroupOpenIDs []string `json:"group_openids,omitempty"`
}
