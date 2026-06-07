package css

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cacheDirName = "css"

// cmsePageURL 中国载人航天工程办公室轨道参数发布页面。
const cmsePageURL = "https://www.cmse.gov.cn/gfgg/zgkjzgdcs/"

// StateVector 表示 OEM 数据中的一个状态向量（位置 + 速度）。
type StateVector struct {
	Time       time.Time
	X, Y, Z    float64 // 位置 (km, EME2000 坐标系)
	Vx, Vy, Vz float64 // 速度 (km/s)
}

// OEMEphemeris 表示解析后的 CCSDS OEM 轨道数据文件。
type OEMEphemeris struct {
	CreationDate time.Time     // 数据创建时间
	StartTime    time.Time     // 有效起始时间
	StopTime     time.Time     // 有效结束时间
	Vectors      []StateVector // 状态向量序列（4 分钟间隔）
	SourceURL    string        // 下载来源 URL
	DownloadedAt time.Time     // 下载时间
}

// IsValid 报告 OEM 数据是否有效（包含状态向量且时间范围完整）。
func (o *OEMEphemeris) IsValid() bool {
	return len(o.Vectors) > 0 && !o.StartTime.IsZero() && !o.StopTime.IsZero()
}

// Covers 报告给定时间是否在 OEM 数据有效范围内。
func (o *OEMEphemeris) Covers(t time.Time) bool {
	return !t.Before(o.StartTime) && !t.After(o.StopTime)
}

// getLatestDownloadURL 解析 CMSE 页面，提取最新的 ZIP 下载链接。
func getLatestDownloadURL(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cmsePageURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 查找页面中第一个 CSS_OEM_*.zip 下载链接
	html := string(body)
	before, _, ok := strings.Cut(html, "CSS_OEM_")
	if !ok {
		return "", fmt.Errorf("no OEM download link found on CMSE page")
	}

	start := strings.LastIndex(before, `href="`)
	if start < 0 {
		return "", fmt.Errorf("cannot parse download link href")
	}
	start += 6 // skip href="
	end := strings.Index(html[start:], `"`)
	if end < 0 {
		return "", fmt.Errorf("cannot parse download link end")
	}
	link := html[start : start+end]

	base, err := url.Parse(cmsePageURL)
	if err != nil {
		return "", err
	}
	absURL, err := base.Parse(link)
	if err != nil {
		return "", err
	}
	return absURL.String(), nil
}

// downloadAndParse 下载 ZIP 文件并解析其中的 OEM 数据。
func downloadAndParse(ctx context.Context, downloadURL string) (*OEMEphemeris, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	zipData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, err
	}

	// 查找 ZIP 中的 .dat 文件
	var datFile *zip.File
	for _, f := range zipReader.File {
		if strings.HasSuffix(f.Name, ".dat") || strings.HasPrefix(f.Name, "CSS_OEM_") {
			datFile = f
			break
		}
	}
	if datFile == nil {
		return nil, fmt.Errorf("no OEM .dat file found in ZIP")
	}

	f, err := datFile.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	return parseOEM(content)
}

// parseOEM 解析 CCSDS OEM 格式文本内容为结构化的轨道数据。
//
// OEM 格式示例:
//
//	CCSDS_OEM_VERS = 2.0
//	CREATION_DATE  = 2026-06-05T00:24:32
//	META_START
//	OBJECT_NAME    = KJZ
//	OBJECT_ID      = CSS
//	REF_FRAME      = EME2000
//	TIME_SYSTEM    = UTC
//	START_TIME     = 2026-06-05T00:00:00.000000
//	STOP_TIME      = 2026-06-12T00:00:00.000000
//	META_STOP
//	COMMENT OEM data, unit is km and km/s.
//	2026-06-05T00:00:00.000000       X(km)       Y(km)       Z(km)      Vx(km/s)    Vy(km/s)    Vz(km/s)
func parseOEM(data []byte) (*OEMEphemeris, error) {
	oem := &OEMEphemeris{
		DownloadedAt: time.Now(),
	}

	lines := strings.Split(string(data), "\n")
	inMeta := false
	metaDone := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "META_START") {
			inMeta = true
			continue
		}
		if strings.HasPrefix(line, "META_STOP") {
			inMeta = false
			metaDone = true
			continue
		}

		if inMeta {
			if strings.HasPrefix(line, "CREATION_DATE") {
				t, err := parseOEMTime(extractValue(line))
				if err == nil {
					oem.CreationDate = t
				}
				continue
			}
			if strings.HasPrefix(line, "START_TIME") {
				t, err := parseOEMTime(extractValue(line))
				if err == nil {
					oem.StartTime = t
				}
				continue
			}
			if strings.HasPrefix(line, "STOP_TIME") {
				t, err := parseOEMTime(extractValue(line))
				if err == nil {
					oem.StopTime = t
				}
				continue
			}
			continue
		}

		if strings.HasPrefix(line, "COMMENT") {
			continue
		}
		if !metaDone {
			continue
		}

		// 数据行：时间  X  Y  Z  Vx  Vy  Vz
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}

		t, err := time.Parse("2006-01-02T15:04:05.000000", fields[0])
		if err != nil {
			continue
		}

		sv := StateVector{Time: t}
		fmt.Sscanf(fields[1], "%f", &sv.X)
		fmt.Sscanf(fields[2], "%f", &sv.Y)
		fmt.Sscanf(fields[3], "%f", &sv.Z)
		fmt.Sscanf(fields[4], "%f", &sv.Vx)
		fmt.Sscanf(fields[5], "%f", &sv.Vy)
		fmt.Sscanf(fields[6], "%f", &sv.Vz)

		oem.Vectors = append(oem.Vectors, sv)
	}

	if len(oem.Vectors) == 0 {
		return nil, fmt.Errorf("no state vectors found in OEM data")
	}

	return oem, nil
}

// extractValue 从 "KEY = VALUE" 格式的行中提取 VALUE。
func extractValue(line string) string {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(parts[1])
}

// parseOEMTime 解析 OEM 文件中的时间戳（支持多种格式）。
func parseOEMTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000000",
	}
	for _, f := range formats {
		t, err := time.Parse(f, s)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

// oemMeta 缓存元数据，与 OEMEphemeris 的非向量字段一一对应。
type oemMeta struct {
	CreationDate time.Time `json:"creation_date"`
	StartTime    time.Time `json:"start_time"`
	StopTime     time.Time `json:"stop_time"`
	SourceURL    string    `json:"source_url"`
	DownloadedAt time.Time `json:"downloaded_at"`
}

// SaveCache 将 OEM 数据缓存到磁盘。
// oem.dat 保存原始文本内容，meta.json 保存非向量元数据。
func (o *OEMEphemeris) SaveCache(baseDir string) error {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return err
	}

	// 重建原始文本以缓存
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("CCSDS_OEM_VERS = 2.0\n"))
	buf.WriteString(fmt.Sprintf("CREATION_DATE  = %s\n", o.CreationDate.Format("2006-01-02T15:04:05")))
	buf.WriteString(fmt.Sprintf("ORIGINATOR     = BACC\n\n"))
	buf.WriteString(fmt.Sprintf("META_START\n"))
	buf.WriteString(fmt.Sprintf("OBJECT_NAME    = KJZ\n"))
	buf.WriteString(fmt.Sprintf("OBJECT_ID      = CSS\n"))
	buf.WriteString(fmt.Sprintf("CENTER_NAME    = EARTH\n"))
	buf.WriteString(fmt.Sprintf("REF_FRAME      = EME2000\n"))
	buf.WriteString(fmt.Sprintf("TIME_SYSTEM    = UTC\n"))
	buf.WriteString(fmt.Sprintf("START_TIME     = %s\n", o.StartTime.Format("2006-01-02T15:04:05.000000")))
	buf.WriteString(fmt.Sprintf("STOP_TIME      = %s\n", o.StopTime.Format("2006-01-02T15:04:05.000000")))
	buf.WriteString(fmt.Sprintf("META_STOP\n\n"))
	buf.WriteString(fmt.Sprintf("COMMENT OEM data, unit is km and km/s.\n"))

	for _, v := range o.Vectors {
		buf.WriteString(fmt.Sprintf("%s       %.12f    %.12f    %.12f    %.12f    %.12f    %.12f\n",
			v.Time.Format("2006-01-02T15:04:05.000000"),
			v.X, v.Y, v.Z, v.Vx, v.Vy, v.Vz))
	}

	datPath := filepath.Join(baseDir, "oem.dat")
	if err := os.WriteFile(datPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("save oem.dat: %w", err)
	}

	meta := oemMeta{
		CreationDate: o.CreationDate,
		StartTime:    o.StartTime,
		StopTime:     o.StopTime,
		SourceURL:    o.SourceURL,
		DownloadedAt: o.DownloadedAt,
	}
	metaPath := filepath.Join(baseDir, "meta.json")
	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("save meta.json: %w", err)
	}

	return nil
}

// LoadCache 从磁盘缓存加载 OEM 轨道数据。
// 返回 nil 表示缓存不存在或已损坏。
func LoadCache(baseDir string) *OEMEphemeris {
	datPath := filepath.Join(baseDir, "oem.dat")
	metaPath := filepath.Join(baseDir, "meta.json")

	oemData, err := os.ReadFile(datPath)
	if err != nil {
		return nil
	}

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil
	}

	var meta oemMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil
	}

	oem, err := parseOEM(oemData)
	if err != nil {
		return nil
	}

	oem.CreationDate = meta.CreationDate
	oem.StartTime = meta.StartTime
	oem.StopTime = meta.StopTime
	oem.SourceURL = meta.SourceURL
	oem.DownloadedAt = meta.DownloadedAt

	return oem
}

// downloadAndParseWithCache 下载 ZIP 并解析，成功后写入缓存。
func downloadAndParseWithCache(ctx context.Context, downloadURL, cacheDir string) (*OEMEphemeris, error) {
	oem, err := downloadAndParse(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	oem.SourceURL = downloadURL
	if cacheDir != "" {
		if err := oem.SaveCache(cacheDir); err != nil {
			return oem, err
		}
	}
	return oem, nil
}
