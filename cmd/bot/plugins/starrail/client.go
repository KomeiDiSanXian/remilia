package starrail

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"sync"
)

const (
	mihomoAPI = "https://api.mihomo.me/sr_info_parsed/%s?lang=cn"
)

// MihomoPlayer 表示 mihomo.me API 返回的玩家基本信息。
type MihomoPlayer struct {
	UID        string `json:"uid"`
	Nickname   string `json:"nickname"`
	Level      int    `json:"level"`
	WorldLevel int    `json:"world_level"`
	Signature  string `json:"signature"`
	Avatar     struct {
		ID   string `json:"id"`
		Icon string `json:"icon"`
	} `json:"avatar"`
	Friends int `json:"friends"`
}

// MihomoElement 表示角色的属性类型（火/冰/雷等）。
type MihomoElement struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
}

// MihomoPath 表示角色的命途（巡猎/毁灭等）。
type MihomoPath struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// MihomoSkill 表示角色的一个行迹/技能。
type MihomoSkill struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Level  int    `json:"level"`
	Desc   string `json:"desc"`
	Icon   string `json:"icon"`
	IsUlt  bool   `json:"is_ult"`
}

// MihomoEidon 表示角色的一个星魂。
type MihomoEidon struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Desc        string `json:"desc"`
	Level       int    `json:"level"`
	IsActivated bool   `json:"is_activated"`
}

// MihomoLightCone 表示角色的光锥装备。
type MihomoLightCone struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Rarity    int    `json:"rarity"`
	Level     int    `json:"level"`
	Promotion int    `json:"promotion"`
	Rank      int    `json:"rank"`
	Icon      string `json:"icon"`
	Preview   string `json:"preview"`
}

// MihomoRelic 表示角色的遗器装备。
type MihomoRelic struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SetID    string `json:"set_id"`
	SetName  string `json:"set_name"`
	Rarity   int    `json:"rarity"`
	Level    int    `json:"level"`
	Icon     string `json:"icon"`
	MainProp struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Value float64 `json:"value"`
	} `json:"main_prop"`
	SubProps []struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Value float64 `json:"value"`
	} `json:"sub_props"`
}

// MihomoRelicSet 表示遗器套装信息。
type MihomoRelicSet struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Num  int    `json:"num"`
	Icon string `json:"icon"`
}

// MihomoCharacter 表示 mihomo.me API 返回的角色详细信息。
type MihomoCharacter struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Rarity    int             `json:"rarity"`
	Level     int             `json:"level"`
	Promotion int             `json:"promotion"`
	Icon      string          `json:"icon"`
	Preview   string          `json:"preview"`
	Portrait  string          `json:"portrait"`
	Element   MihomoElement   `json:"element"`
	Path      MihomoPath     `json:"path"`
	HP        float64         `json:"hp"`
	ATK       float64         `json:"atk"`
	DEF       float64         `json:"def"`
	Speed     float64         `json:"speed"`
	CritRate  float64         `json:"crit_rate"`
	CritDMG   float64         `json:"crit_dmg"`
	BreakDMG  float64         `json:"break_dmg"`
	EnergyRegen float64       `json:"energy_regen"`
	Skills    []MihomoSkill   `json:"skills"`
	Eidons    []MihomoEidon   `json:"eidons"`
	LightCone *MihomoLightCone `json:"light_cone"`
	Relics    []MihomoRelic   `json:"relics"`
	RelicSets map[string]int  `json:"relic_sets"`
}

// MihomoResponse 是 mihomo.me API 的完整响应结构。
type MihomoResponse struct {
	UID        string             `json:"uid"`
	Player     MihomoPlayer       `json:"player"`
	Characters []MihomoCharacter  `json:"characters"`
}

// HSRShowcase 是经过解析的星穹铁道玩家展柜数据。
type HSRShowcase struct {
	Player MihomoPlayer
	Chars  []HSRShowcaseChar
}

// HSRShowcaseChar 表示展柜中一个角色的解析后数据。
type HSRShowcaseChar struct {
	ID        string
	Name      string
	Rarity    int
	Level     int
	Element   string
	Path      string
	IconURL   string
	IconImage image.Image
	Eidolon   int
	Skills    []int
	LightCone *HSRLightCone
	Relics    []HSRRelic
	RelicSets map[string]int
}

// HSRLightCone 表示解析后的角色光锥信息。
type HSRLightCone struct {
	Name   string
	Rarity int
	Level  int
	Rank   int
	IconURL string
}

// HSRRelic 表示解析后的遗器信息。
type HSRRelic struct {
	Name    string
	SetName string
	Level   int
	IconURL string
}

// FetchShowcase 查询指定 UID 的星穹铁道开拓者角色展柜。
// 返回的数据已由 mihomo.me API 解析为中文名，无需额外本地化映射。
func FetchShowcase(ctx context.Context, uid string) (*HSRShowcase, error) {
	url := fmt.Sprintf(mihomoAPI, uid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("用户不存在")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var data MihomoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}

	result := &HSRShowcase{Player: data.Player}
	for _, c := range data.Characters {
		sc := HSRShowcaseChar{
			ID:        c.ID,
			Name:      c.Name,
			Rarity:    c.Rarity,
			Level:     c.Level,
			Element:   c.Element.Name,
			Path:      c.Path.Name,
			IconURL:   c.Icon,
			IconImage: nil,
			Eidolon:   countActivatedEidons(c.Eidons),
			RelicSets: c.RelicSets,
		}
		for _, s := range c.Skills {
			sc.Skills = append(sc.Skills, s.Level)
		}
		if c.LightCone != nil {
			sc.LightCone = &HSRLightCone{
				Name:    c.LightCone.Name,
				Rarity:  c.LightCone.Rarity,
				Level:   c.LightCone.Level,
				Rank:    c.LightCone.Rank,
				IconURL: c.LightCone.Icon,
			}
		}
		for _, r := range c.Relics {
			sc.Relics = append(sc.Relics, HSRRelic{
				Name:    r.Name,
				SetName: r.SetName,
				Level:   r.Level,
				IconURL: r.Icon,
			})
		}
		result.Chars = append(result.Chars, sc)
	}

	var wg sync.WaitGroup
	for i := range result.Chars {
		if result.Chars[i].IconURL == "" {
			continue
		}
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			img, e := fetchImage(ctx, url)
			if e == nil {
				result.Chars[idx].IconImage = img
			}
		}(i, result.Chars[i].IconURL)
	}
	wg.Wait()

	return result, nil
}

func countActivatedEidons(eidons []MihomoEidon) int {
	count := 0
	for _, e := range eidons {
		if e.IsActivated {
			count++
		}
	}
	return count
}

func fetchImage(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	return img, err
}
