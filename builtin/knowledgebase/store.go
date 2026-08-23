package knowledgebase

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Chunk 知识库分块（SQLite 持久化，向量以 float32 LE 字节流存储）。
type Chunk struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Source    string `gorm:"index"` // 源文件相对路径，如 06-plugins/COMMAND_SYSTEM.md
	Heading   string // 所属 Markdown 标题（检索展示用）
	Content   string `gorm:"type:text"`
	Hash      string `gorm:"index"` // content sha256（增量更新检测）
	Vector    []byte // float32 LE 序列；空表示未嵌入
	Dim       int    // 向量维度
	Model     string // 生成向量的模型名
	UpdatedAt int64
}

// SourceFile 源文件索引状态（文件 hash 变化时增量重建）。
type SourceFile struct {
	Source     string `gorm:"primaryKey"`
	Hash       string // 文件内容 sha256
	ChunkCount int
	UpdatedAt  int64
}

// indexedChunk 内存索引条目（检索时直接打分）。
type indexedChunk struct {
	ID      int64
	Source  string
	Heading string
	Content string
	Vector  []float32
}

// Store SQLite 索引存储。
type Store struct {
	db *gorm.DB
}

// OpenStore 打开或创建知识库索引数据库，自动建表。
func OpenStore(path string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode = WAL")
	db.Exec("PRAGMA synchronous = NORMAL")
	db.Exec("PRAGMA auto_vacuum = INCREMENTAL")
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&Chunk{}, &SourceFile{}); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ReplaceSource 原子替换一个源文件的全部分块并记录文件 hash。
func (s *Store) ReplaceSource(source, hash string, chunks []Chunk) error {
	now := time.Now().Unix()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source = ?", source).Delete(&Chunk{}).Error; err != nil {
			return err
		}
		if len(chunks) > 0 {
			for i := range chunks {
				chunks[i].UpdatedAt = now
			}
			if err := tx.Create(&chunks).Error; err != nil {
				return err
			}
		}
		sf := SourceFile{Source: source, Hash: hash, ChunkCount: len(chunks), UpdatedAt: now}
		return tx.Save(&sf).Error
	})
}

// RemoveSource 删除已不在磁盘上的源文件及其分块。
func (s *Store) RemoveSource(source string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source = ?", source).Delete(&Chunk{}).Error; err != nil {
			return err
		}
		return tx.Where("source = ?", source).Delete(&SourceFile{}).Error
	})
}

// SourceFiles 返回全部源文件的索引状态。
func (s *Store) SourceFiles() ([]SourceFile, error) {
	var files []SourceFile
	if err := s.db.Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

// LoadIndex 加载全部分块到内存（docs 规模 ~千级 chunk，内存占用可忽略）。
func (s *Store) LoadIndex() ([]indexedChunk, error) {
	var rows []Chunk
	if err := s.db.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]indexedChunk, 0, len(rows))
	for _, r := range rows {
		ic := indexedChunk{
			ID:      r.ID,
			Source:  r.Source,
			Heading: r.Heading,
			Content: r.Content,
		}
		if len(r.Vector) > 0 && r.Dim > 0 {
			ic.Vector = decodeVector(r.Vector, r.Dim)
		}
		out = append(out, ic)
	}
	return out, nil
}

// CountChunks 返回分块总数。
func (s *Store) CountChunks() (int64, error) {
	var n int64
	err := s.db.Model(&Chunk{}).Count(&n).Error
	return n, err
}

// Clear 清空全部索引（/kb rebuild 全量模式使用）。
func (s *Store) Clear() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&Chunk{}).Error; err != nil {
			return err
		}
		return tx.Where("1 = 1").Delete(&SourceFile{}).Error
	})
}

// encodeVector 将 float32 向量编码为 LE 字节流。
func encodeVector(v []float32) []byte {
	out := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}
	return out
}

// decodeVector 从 LE 字节流解码 float32 向量。
func decodeVector(b []byte, dim int) []float32 {
	out := make([]float32, 0, dim)
	for i := 0; i+4 <= len(b) && len(out) < dim; i += 4 {
		out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(b[i:])))
	}
	return out
}
