# 地图数据来源与授权

本目录下的 `world_coastline.bin` 供地面轨迹地图（`GroundTrackPanel`）描绘世界海岸线
轮廓。数据随源码编译进二进制（见 `../coastline.go` 的 `//go:embed`），运行时不访问
任何外部服务。

## world_coastline.bin — 世界海岸线轮廓

- 内容：134 条海岸线折线，共 5128 个顶点
- 来源：Natural Earth 1:110m 物理数据 `ne_110m_coastline`
  <https://github.com/nvkelso/natural-earth-vector>
  （下载自 <https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_110m_coastline.geojson>）
- 授权：公有领域（Natural Earth 数据无需署名，此处为可追溯性而记录）
- 处理：
  - 坐标保持 WGS84 经纬度原值，按 1e-2 度量化后以小端 int16 存储
    （最大误差约 0.6 km，相对卡片上约 55 km/像素的比例完全不可见）
  - 保留全部原始顶点，未做任何简化，以避免大陆轮廓形状失真
  - 折线在 ±180° 经线处断开，便于逐段描绘

文件格式见 `../coastline.go` 中 `decodeCoastlines` 的说明。
