// Package kv 提供基于 LevelDB 的键值存储抽象。
package kv

import (
	"errors"

	"github.com/syndtr/goleveldb/leveldb"
)

// ErrNotFound 键不存在时返回。
var ErrNotFound = errors.New("key not found")

// Store 键值存储接口。
type Store interface {
	Get(key []byte) ([]byte, error)
	Set(key, val []byte) error
	Delete(key []byte) error
	Close() error
}

// DB 基于 LevelDB 的键值存储实现。
type DB struct {
	db *leveldb.DB
}

// Open 打开指定路径的 LevelDB 数据库。
func Open(path string) (*DB, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, err
	}
	return &DB{db: db}, nil
}

// Get 获取指定键的值。键不存在时返回 ErrNotFound。
func (d *DB) Get(key []byte) ([]byte, error) {
	val, err := d.db.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, ErrNotFound
	}
	return val, err
}

// Set 设置键值对。
func (d *DB) Set(key, val []byte) error {
	return d.db.Put(key, val, nil)
}

// Delete 删除指定键。键不存在时返回 nil。
func (d *DB) Delete(key []byte) error {
	return d.db.Delete(key, nil)
}

// Close 关闭数据库。
func (d *DB) Close() error {
	return d.db.Close()
}
