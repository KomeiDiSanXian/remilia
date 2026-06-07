package iss

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// IssPosition ISS 位置、高度和速度信息。
type IssPosition struct {
	Latitude   float64
	Longitude  float64
	Altitude   float64
	Velocity   float64 // km/h
	Visibility string  // daylight / eclipsed
	Timestamp  time.Time
}

// issPositionResponse API 位置响应数据结构。
type issPositionResponse struct {
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Altitude   float64 `json:"altitude"`
	Velocity   float64 `json:"velocity"`
	Visibility string  `json:"visibility"`
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
}
// astrosResponse API 在轨航天员响应数据结构。
type astrosResponse struct {
	Number int `json:"number"`
	People []struct {
		Name  string `json:"name"`
		Craft string `json:"craft"`
	} `json:"people"`
}

// fetchPosition 从 wheretheiss.at API 获取 ISS 当前位置（经纬度、高度、速度）。
func fetchPosition(ctx context.Context) (*IssPosition, error) {
	u := "https://api.wheretheiss.at/v1/satellites/25544"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data issPositionResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	vis := data.Visibility
	if vis == "" {
		vis = "unknown"
	}
	return &IssPosition{
		Latitude:   data.Latitude,
		Longitude:  data.Longitude,
		Altitude:   data.Altitude,
		Velocity:   data.Velocity,
		Visibility: vis,
		Timestamp:  time.Now().UTC(),
	}, nil
}

// fetchAstros 从 open-notify.org API 获取当前在轨航天员列表。
func fetchAstros(ctx context.Context) ([]string, int, error) {
	u := "http://api.open-notify.org/astros.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var data astrosResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, 0, err
	}
	names := make([]string, 0, len(data.People))
	for _, p := range data.People {
		if p.Craft == "ISS" {
			names = append(names, p.Name)
		}
	}
	return names, data.Number, nil
}

// fmtLat 将纬度格式化为带 N/S 方向的字符串。
func fmtLat(lat float64) string {
	dir := "N"
	if lat < 0 {
		dir = "S"
		lat = -lat
	}
	return fmt.Sprintf("%.4f°%s", lat, dir)
}

// fmtLng 将经度格式化为带 E/W 方向的字符串。
func fmtLng(lng float64) string {
	dir := "E"
	if lng < 0 {
		dir = "W"
		lng = -lng
	}
	return fmt.Sprintf("%.4f°%s", lng, dir)
}
