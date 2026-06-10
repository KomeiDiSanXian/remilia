package genshin

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	enkaStoreCharacters = "https://raw.githubusercontent.com/EnkaNetwork/API-docs/master/store/characters.json"
	enkaStoreLoc        = "https://raw.githubusercontent.com/EnkaNetwork/API-docs/master/store/loc.json"
	enkaUIDAPI          = "https://enka.network/api/uid/%s/"
	enkaImageBase       = "https://enka.network/ui/"
)

type enkaStore struct {
	mu         sync.RWMutex
	characters map[string]enkaCharacter
	loc        map[string]string
	refreshed  time.Time
}

type enkaCharacter struct {
	NameTextMapHash string `json:"NameTextMapHash"`
	SideIconName    string `json:"SideIconName"`
	Element         string `json:"Element"`
	WeaponType      string `json:"WeaponType"`
	QualityType     string `json:"QualityType"`
}

// EnkaPlayerInfo 表示 Enka API 返回的玩家基本信息。
type EnkaPlayerInfo struct {
	Nickname         string `json:"nickname"`
	Level            int    `json:"level"`
	Signature        string `json:"signature"`
	WorldLevel       int    `json:"worldLevel"`
	NameCardID       int    `json:"nameCardId"`
	FinishAchievementNum int `json:"finishAchievementNum"`
	ProfilePicture   struct {
		AvatarID int `json:"avatarId"`
	} `json:"profilePicture"`
	ShowAvatarInfoList []EnkaShowAvatar `json:"showAvatarInfoList"`
}

// EnkaShowAvatar 表示展柜中一个角色的概要信息。
type EnkaShowAvatar struct {
	AvatarID int `json:"avatarId"`
	Level    int `json:"level"`
}

// EnkaAPIResponse 是 Enka Network API 返回的完整响应结构。
type EnkaAPIResponse struct {
	UID            string           `json:"uid"`
	PlayerInfo     EnkaPlayerInfo   `json:"playerInfo"`
	AvatarInfoList []EnkaAvatarInfo `json:"avatarInfoList"`
	TTL            int              `json:"ttl"`
	Region         string           `json:"region"`
}

// EnkaAvatarInfo 表示 Enka API 返回的单个角色详细信息。
type EnkaAvatarInfo struct {
	AvatarID        int                            `json:"avatarId"`
	PropMap         map[string]json.RawMessage     `json:"propMap"`
	FightPropMap    map[string]float64             `json:"fightPropMap"`
	SkillLevelMap   map[string]int                 `json:"skillLevelMap"`
	EquipList       []EnkaEquip                    `json:"equipList"`
	TalentIDList    []int                          `json:"talentIdList"`
	InherentProudMap map[string]int                `json:"inherentProudMap"`
	SkillDepotID    int                            `json:"skillDepotId"`
}

// EnkaEquip 表示角色装备的一个物品（武器或圣遗物）。
type EnkaEquip struct {
	ItemType  string           `json:"itemType"`
	Flat      json.RawMessage  `json:"flat"`
	Weapon    *EnkaWeapon      `json:"weapon,omitempty"`
	Reliquary *json.RawMessage `json:"reliquary,omitempty"`
}

// EnkaWeapon 表示 Enka API 中的武器数据。
type EnkaWeapon struct {
	Level        int           `json:"level"`
	PromoteLevel int           `json:"promoteLevel"`
	AffixMap     map[string]int `json:"affixMap"`
}

// EnkaFlat 表示装备的平铺信息（名称、图标、等级等）。
type EnkaFlat struct {
	NameTextMapHash    string     `json:"nameTextMapHash"`
	Icon               string     `json:"icon"`
	ItemType           string     `json:"itemType"`
	RankLevel          int        `json:"rankLevel"`
	WeaponStats        []EnkaStat `json:"weaponStats,omitempty"`
	ReliquarySubstats  []EnkaStat `json:"reliquarySubstats,omitempty"`
	ReliquaryMainstat  *EnkaStat  `json:"reliquaryMainstat,omitempty"`
	SetNameTextMapHash string     `json:"setNameTextMapHash,omitempty"`
}

// EnkaStat 表示一个词条属性（如攻击力百分比 +5.3%）。
type EnkaStat struct {
	AppendPropID string  `json:"appendPropId"`
	StatValue    float64 `json:"statValue"`
}

// GenshinShowcase 是经过解析的完整原神玩家展柜数据。
type GenshinShowcase struct {
	Player EnkaPlayerInfo
	Chars  []GenshinShowcaseChar
	Region string
}

// GenshinShowcaseChar 表示展柜中一个角色的解析后数据。
type GenshinShowcaseChar struct {
	AvatarID      int
	Name          string
	IconURL       string
	Element       string
	Level         int
	Constellation int
	Talents       []int
	Weapon        *GenshinWeapon
	Artifacts     []GenshinArtifact
	IconImage     image.Image
}

// GenshinWeapon 表示解析后的角色武器信息。
type GenshinWeapon struct {
	Name       string
	IconURL    string
	Level      int
	Refinement int
	RankLevel  int
}

// GenshinArtifact 表示解析后的圣遗物信息。
type GenshinArtifact struct {
	Name     string
	IconURL  string
	SetName  string
	RankLevel int
	MainStat *EnkaStat
	SubStats []EnkaStat
}

var defaultStore = &enkaStore{}

func (s *enkaStore) refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.refreshed) < 10*time.Minute {
		return nil
	}

	chars, err := fetchJSON[map[string]enkaCharacter](ctx, enkaStoreCharacters)
	if err != nil {
		return fmt.Errorf("fetch characters: %w", err)
	}

	rawLoc, err := fetchRawJSON(ctx, enkaStoreLoc)
	if err != nil {
		return fmt.Errorf("fetch loc: %w", err)
	}

	locCN := extractCNLoc(rawLoc)

	s.characters = *chars
	s.loc = locCN
	s.refreshed = time.Now()
	return nil
}

func extractCNLoc(raw json.RawMessage) map[string]string {
	var all map[string]map[string]string
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil
	}
	if cn, ok := all["cn"]; ok && cn != nil {
		return cn
	}
	return all["en"]
}

func (s *enkaStore) resolveName(hash string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.loc == nil {
		return ""
	}
	return s.loc[hash]
}

func (s *enkaStore) resolveChar(avatarID int) *enkaCharacter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id := fmt.Sprintf("%d", avatarID)
	c, ok := s.characters[id]
	if !ok {
		return nil
	}
	return &c
}

func (s *enkaStore) imageURL(iconName string) string {
	return enkaImageBase + iconName + ".png"
}

// FetchShowcase 查询指定 UID 的原神玩家角色展柜。
// 内部自动缓存 Enka Store 数据（10 分钟 TTL）用于角色名称解析。
func FetchShowcase(ctx context.Context, uid string) (*GenshinShowcase, error) {
	if err := defaultStore.refresh(ctx); err != nil {
		return nil, err
	}

	resp, err := fetchJSON[EnkaAPIResponse](ctx, fmt.Sprintf(enkaUIDAPI, uid))
	if err != nil {
		return nil, fmt.Errorf("fetch uid: %w", err)
	}

	result := &GenshinShowcase{
		Player: resp.PlayerInfo,
		Region: resp.Region,
	}

	for _, av := range resp.AvatarInfoList {
		char := defaultStore.resolveChar(av.AvatarID)
		showChar := GenshinShowcaseChar{
			AvatarID: av.AvatarID,
			Level:    extractPropInt(av.PropMap, "level"),
			Element:  detectElement(av.FightPropMap),
		}

		if char != nil {
			showChar.Name = defaultStore.resolveName(char.NameTextMapHash)
			if showChar.Name == "" {
				showChar.Name = char.NameTextMapHash
			}
			showChar.IconURL = defaultStore.imageURL(char.SideIconName)
		}
		if showChar.Name == "" {
			showChar.Name = fmt.Sprintf("角色 %d", av.AvatarID)
		}

		if av.TalentIDList != nil {
			showChar.Constellation = len(av.TalentIDList)
		}

		{
			keys := make([]string, 0, len(av.SkillLevelMap))
			for k := range av.SkillLevelMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys[:min(3, len(keys))] {
				showChar.Talents = append(showChar.Talents, av.SkillLevelMap[k])
			}
		}

		for _, eq := range av.EquipList {
			switch eq.ItemType {
			case "ITEM_WEAPON":
				w := parseWeapon(eq, defaultStore)
				if w != nil {
					showChar.Weapon = w
				}
			case "ITEM_RELIQUARY":
				a := parseArtifact(eq, defaultStore)
				if a != nil {
					showChar.Artifacts = append(showChar.Artifacts, *a)
				}
			}
		}

		result.Chars = append(result.Chars, showChar)
	}

	return result, nil
}

func parseWeapon(eq EnkaEquip, s *enkaStore) *GenshinWeapon {
	var flat EnkaFlat
	if err := json.Unmarshal(eq.Flat, &flat); err != nil || eq.Weapon == nil {
		return nil
	}
	w := &GenshinWeapon{
		Name:      s.resolveName(flat.NameTextMapHash),
		IconURL:   s.imageURL(flat.Icon),
		Level:     eq.Weapon.Level,
		RankLevel: flat.RankLevel,
	}
	if w.Name == "" {
		w.Name = flat.NameTextMapHash
	}
	for _, v := range eq.Weapon.AffixMap {
		w.Refinement = v
	}
	return w
}

func parseArtifact(eq EnkaEquip, s *enkaStore) *GenshinArtifact {
	var flat EnkaFlat
	if err := json.Unmarshal(eq.Flat, &flat); err != nil {
		return nil
	}
	a := &GenshinArtifact{
		Name:      s.resolveName(flat.NameTextMapHash),
		IconURL:   s.imageURL(flat.Icon),
		RankLevel: flat.RankLevel,
		MainStat:  flat.ReliquaryMainstat,
		SubStats:  flat.ReliquarySubstats,
	}
	if a.Name == "" {
		a.Name = flat.NameTextMapHash
	}
	if flat.SetNameTextMapHash != "" {
		a.SetName = s.resolveName(flat.SetNameTextMapHash)
	}
	return a
}

type enkaPropValue struct {
	Type int    `json:"type"`
	Val  string `json:"val"`
	IVal int    `json:"ival"`
}

func extractPropInt(m map[string]json.RawMessage, key string) int {
	if m == nil {
		return 0
	}
	raw, ok := m[key]
	if !ok {
		return 0
	}
	var p enkaPropValue
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0
	}
	if p.IVal > 0 {
		return p.IVal
	}
	var v int
	fmt.Sscanf(p.Val, "%d", &v)
	return v
}

func detectElement(fp map[string]float64) string {
	if fp == nil {
		return "未知"
	}
	for k := range fp {
		switch k {
		case "50":
			return "火"
		case "40":
			return "雷"
		case "51":
			return "水"
		case "60":
			return "冰"
		case "70":
			return "风"
		case "80":
			return "岩"
		case "90":
			return "草"
		}
	}
	return "未知"
}

func fetchJSON[T any](ctx context.Context, url string) (*T, error) {
	raw, err := fetchRawJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	return &result, nil
}

func fetchRawJSON(ctx context.Context, url string) (json.RawMessage, error) {
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
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}


