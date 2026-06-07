package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ── wttr.in 客户端 ────────────────────────────────────────────────────

type wttrResponse struct {
	CurrentCondition []struct {
		TempC       string `json:"temp_C"`
		FeelsLikeC  string `json:"FeelsLikeC"`
		Humidity    string `json:"humidity"`
		WindSpeed   string `json:"windspeedKmph"`
		WindDir     string `json:"winddir16Point"`
		Pressure    string `json:"pressure"`
		WeatherDesc []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
		Visibility string `json:"visibility"`
		UVIndex    string `json:"uvIndex"`
		Cloudcover string `json:"cloudcover"`
	} `json:"current_condition"`
	NearestArea []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
	} `json:"nearest_area"`
}

// fetchWttr 从 wttr.in 获取天气数据（免费，无需 API Key）。
func fetchWttr(ctx context.Context, city string) (*Result, error) {
	u := fmt.Sprintf("https://wttr.in/%s?format=j1&lang=zh", url.QueryEscape(city))
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
	var data wttrResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if len(data.CurrentCondition) == 0 {
		return nil, fmt.Errorf("no weather data for %s", city)
	}
	cc := data.CurrentCondition[0]
	r := &Result{
		TempC:         parseFloat(cc.TempC),
		FeelsLikeC:    parseFloat(cc.FeelsLikeC),
		Humidity:      parseInt(cc.Humidity),
		WindSpeedKmph: parseFloat(cc.WindSpeed),
		WindDir:       cc.WindDir,
		PressureMB:    parseFloat(cc.Pressure),
		VisibilityKM:  parseFloat(cc.Visibility),
		UV:            parseInt(cc.UVIndex),
		Cloud:         parseInt(cc.Cloudcover),
	}
	if len(cc.WeatherDesc) > 0 {
		r.Condition = cc.WeatherDesc[0].Value
	}
	if len(data.NearestArea) > 0 && len(data.NearestArea[0].AreaName) > 0 {
		r.City = data.NearestArea[0].AreaName[0].Value
	} else {
		r.City = city
	}
	return r, nil
}

// ── Open-Meteo 客户端 ─────────────────────────────────────────────────

type omGeoResponse struct {
	Results []struct {
		Name      string  `json:"name"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Country   string  `json:"country"`
	} `json:"results"`
}

type omWeatherResponse struct {
	Current struct {
		Temperature  float64 `json:"temperature_2m"`
		Humidity     float64 `json:"relative_humidity_2m"`
		ApparentTemp float64 `json:"apparent_temperature"`
		WindSpeed    float64 `json:"wind_speed_10m"`
		WindDir      float64 `json:"wind_direction_10m"`
		Pressure     float64 `json:"pressure_msl"`
		WeatherCode  float64 `json:"weather_code"`
		Visibility   float64 `json:"visibility"`
		CloudCover   float64 `json:"cloud_cover"`
		UV           float64 `json:"uv_index"`
	} `json:"current"`
}

// fetchOpenMeteo 从 Open-Meteo 获取天气数据（免费，无需 API Key）。
// 先通过 Geocoding API 解析城市坐标，再查询天气。
func fetchOpenMeteo(ctx context.Context, city string) (*Result, error) {
	geoURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=zh&format=json", url.QueryEscape(city))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geoURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var geo omGeoResponse
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return nil, err
	}
	if len(geo.Results) == 0 {
		return nil, fmt.Errorf("city %s not found", city)
	}
	loc := geo.Results[0]

	wURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,apparent_temperature,wind_speed_10m,wind_direction_10m,pressure_msl,weather_code,visibility,cloud_cover,uv_index&timezone=auto",
		loc.Latitude, loc.Longitude)
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, wURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var wData omWeatherResponse
	if err := json.NewDecoder(resp.Body).Decode(&wData); err != nil {
		return nil, err
	}

	return &Result{
		City:          loc.Name,
		TempC:         wData.Current.Temperature,
		FeelsLikeC:    wData.Current.ApparentTemp,
		Humidity:      int(wData.Current.Humidity),
		WindSpeedKmph: wData.Current.WindSpeed * 3.6,
		WindDir:       windDegToDir(wData.Current.WindDir),
		PressureMB:    wData.Current.Pressure,
		Condition:     wmoCodeToDesc(int(wData.Current.WeatherCode)),
		VisibilityKM:  wData.Current.Visibility / 1000,
		UV:            int(wData.Current.UV),
		Cloud:         int(wData.Current.CloudCover),
	}, nil
}

// ── WeatherAPI 客户端 ─────────────────────────────────────────────────

type waResponse struct {
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
	Current struct {
		TempC      float64 `json:"temp_c"`
		FeelsLikeC float64 `json:"feelslike_c"`
		Humidity   int     `json:"humidity"`
		WindKmph   float64 `json:"wind_kph"`
		WindDir    string  `json:"wind_dir"`
		PressureMB float64 `json:"pressure_mb"`
		Condition  struct {
			Text string `json:"text"`
		} `json:"condition"`
		VisKM float64 `json:"vis_km"`
		UV    float64 `json:"uv"`
		Cloud int     `json:"cloud"`
	} `json:"current"`
}

// fetchWeatherAPI 从 WeatherAPI 获取天气数据（需要免费 API Key）。
// Key 未设置时请求将返回 401，由竞速调度自动处理（其他源接力）。
func fetchWeatherAPI(ctx context.Context, city string) (*Result, error) {
	u := fmt.Sprintf("https://api.weatherapi.com/v1/current.json?key=%s&q=%s&lang=zh",
		getWeatherAPIKey(), url.QueryEscape(city))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("WeatherAPI responded with %s", resp.Status)
	}
	var data waResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &Result{
		City:          data.Location.Name,
		TempC:         data.Current.TempC,
		FeelsLikeC:    data.Current.FeelsLikeC,
		Humidity:      data.Current.Humidity,
		WindSpeedKmph: data.Current.WindKmph,
		WindDir:       data.Current.WindDir,
		PressureMB:    data.Current.PressureMB,
		Condition:     data.Current.Condition.Text,
		VisibilityKM:  data.Current.VisKM,
		UV:            int(data.Current.UV),
		Cloud:         data.Current.Cloud,
	}, nil
}

// WeatherAPI Key 全局变量，通过 SetWeatherAPIKey 设置。
var weatherAPIKey string

// SetWeatherAPIKey 设置 WeatherAPI 的 API Key。
// 未设置时该数据源会被竞速调度自动跳过。
func SetWeatherAPIKey(key string) {
	weatherAPIKey = key
}

// getWeatherAPIKey 返回 WeatherAPI Key（可能为空字符串）。
func getWeatherAPIKey() string {
	return weatherAPIKey
}

// ── 工具函数 ──────────────────────────────────────────────────────────

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

// windDegToDir 将风向角度转换为中文方向。
func windDegToDir(deg float64) string {
	dirs := []string{"北", "东北", "东", "东南", "南", "西南", "西", "西北"}
	idx := max(int((deg+22.5)/45), 0)
	if idx >= len(dirs) {
		idx = len(dirs) - 1
	}
	return dirs[idx%len(dirs)]
}

// wmoCodeToDesc 将 WMO 天气代码转换为中文描述。
// 参考: https://open-meteo.com/en/docs#weathervariables
func wmoCodeToDesc(code int) string {
	switch code {
	case 0:
		return "晴天"
	case 1, 2, 3:
		return "多云"
	case 45, 48:
		return "雾"
	case 51, 53, 55:
		return "小雨"
	case 56, 57:
		return "冻雨"
	case 61, 63, 65:
		return "雨"
	case 66, 67:
		return "冻雨"
	case 71, 73, 75:
		return "雪"
	case 77:
		return "雪粒"
	case 80, 81, 82:
		return "阵雨"
	case 85, 86:
		return "阵雪"
	case 95:
		return "雷暴"
	case 96, 99:
		return "冰雹"
	default:
		return "未知"
	}
}
