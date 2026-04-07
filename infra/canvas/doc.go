// 已废弃：infra/canvas 包已合并至 [infra/textimage]。
//
// 请直接使用 textimage 包的矢量绘图功能：
//
//	c := textimage.NewVectorCanvas(800, 600)
//	c.SetRGB(1, 1, 1)
//	c.Clear()
//	c.DrawAvatar(avatarImg, 60, 60, 48)
//	png, _ := c.ToPNG()
//
// 本包保留仅为向后兼容，所有类型均为 textimage 对应类型的别名。
// 本包将在未来版本中移除。
package canvas
