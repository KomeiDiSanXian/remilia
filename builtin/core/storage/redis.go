package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStorage Redis 存储实现，满足 Storage 接口
type RedisStorage struct {
	client    *redis.Client
	keyPrefix string
}

// RedisConfig Redis 连接配置
type RedisConfig struct {
	Addr      string // 地址，如 "localhost:6379"
	Password  string
	DB        int
	KeyPrefix string // 键前缀，避免与其他应用冲突，如 "remilia:"
}

// NewRedisStorage 创建 Redis 存储
func NewRedisStorage(cfg RedisConfig) (*RedisStorage, error) {
	if cfg.Addr == "" {
		cfg.Addr = "localhost:6379"
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "remilia:"
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	// 连通性检查
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return &RedisStorage{client: client, keyPrefix: cfg.KeyPrefix}, nil
}

// Close 关闭 Redis 连接
func (r *RedisStorage) Close() error {
	return r.client.Close()
}

func (r *RedisStorage) prefixed(key string) string {
	return r.keyPrefix + key
}

// Get 获取值
func (r *RedisStorage) Get(key string) ([]byte, error) {
	ctx := context.Background()
	val, err := r.client.Get(ctx, r.prefixed(key)).Bytes()
	if err == redis.Nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	return val, nil
}

// Set 设置值，ttl=0 表示永不过期
func (r *RedisStorage) Set(key string, value []byte, ttl time.Duration) error {
	ctx := context.Background()
	return r.client.Set(ctx, r.prefixed(key), value, ttl).Err()
}

// Delete 删除键
func (r *RedisStorage) Delete(key string) error {
	return r.client.Del(context.Background(), r.prefixed(key)).Err()
}

// Exists 检查键是否存在
func (r *RedisStorage) Exists(key string) bool {
	n, err := r.client.Exists(context.Background(), r.prefixed(key)).Result()
	return err == nil && n > 0
}

// Keys 按 glob 模式列出键（去除前缀后返回）
func (r *RedisStorage) Keys(pattern string) ([]string, error) {
	keys, err := r.client.Keys(context.Background(), r.prefixed(pattern)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis keys: %w", err)
	}
	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, strings.TrimPrefix(k, r.keyPrefix))
	}
	return result, nil
}

// Clear 清空所有以 keyPrefix 开头的键
func (r *RedisStorage) Clear() error {
	ctx := context.Background()
	keys, err := r.client.Keys(ctx, r.keyPrefix+"*").Result()
	if err != nil {
		return fmt.Errorf("redis keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}
