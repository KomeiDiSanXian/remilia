package minecraft

import (
	"errors"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

// favMaxPerScope 每个会话（群/私聊）最多收藏数量。
const favMaxPerScope = 10

// FavServer 收藏的 Minecraft 服务器（按会话：群/私聊）。
type FavServer struct {
	ID        uint   `gorm:"primaryKey"`
	Scope     string `gorm:"uniqueIndex:idx_fav_scope_name"`
	Name      string `gorm:"uniqueIndex:idx_fav_scope_name"`
	Address   string
	CreatedAt time.Time
}

// FavManager 收藏管理（基于 storage 插件；服务不可用时自动禁用收藏功能）。
type FavManager struct {
	db *storage.Plugin
}

// NewFavManager 创建收藏管理器。db 为 nil 时功能禁用。
func NewFavManager(db *storage.Plugin) *FavManager {
	if db == nil {
		return nil
	}
	return &FavManager{db: db}
}

// Available 收藏功能是否可用。
func (f *FavManager) Available() bool { return f != nil && f.db != nil }

// List 返回会话的收藏列表（按添加顺序）。
func (f *FavManager) List(scope string) ([]FavServer, error) {
	if !f.Available() {
		return nil, errors.New("存储服务不可用，收藏功能未启用")
	}
	var favs []FavServer
	err := f.db.Where("scope = ?", scope).Order("id").Find(&favs)
	return favs, err
}

// Add 添加收藏。同名时覆盖地址（返回 created=false）。
func (f *FavManager) Add(scope, name, address string) (created bool, err error) {
	if !f.Available() {
		return false, errors.New("存储服务不可用，收藏功能未启用")
	}
	var existing FavServer
	if err := f.db.Where("scope = ? AND name = ?", scope, name).First(&existing); err == nil {
		existing.Address = address
		return false, f.db.Save(&existing)
	}
	var all []FavServer
	if err := f.db.Where("scope = ?", scope).Find(&all); err == nil && len(all) >= favMaxPerScope {
		return false, fmt.Errorf("每个会话最多收藏 %d 个服务器", favMaxPerScope)
	}
	return true, f.db.Create(&FavServer{Scope: scope, Name: name, Address: address})
}

// Remove 删除收藏。返回是否确实删除（不存在返回 false）。
func (f *FavManager) Remove(scope, name string) (bool, error) {
	if !f.Available() {
		return false, errors.New("存储服务不可用，收藏功能未启用")
	}
	var existing FavServer
	if err := f.db.Where("scope = ? AND name = ?", scope, name).First(&existing); err != nil {
		return false, nil
	}
	return true, f.db.Delete(&existing)
}

// Get 按名称取收藏。
func (f *FavManager) Get(scope, name string) (*FavServer, error) {
	if !f.Available() {
		return nil, errors.New("存储服务不可用，收藏功能未启用")
	}
	var fav FavServer
	if err := f.db.Where("scope = ? AND name = ?", scope, name).First(&fav); err != nil {
		return nil, err
	}
	return &fav, nil
}
