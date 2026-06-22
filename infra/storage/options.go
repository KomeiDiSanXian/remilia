package storage

// DriverType 数据库驱动类型枚举。
type DriverType string

const (
	// DriverSQLite 使用 SQLite（默认，需要 CGO；可替换为 glebarez/sqlite 实现纯 Go）
	DriverSQLite DriverType = "sqlite"
	// DriverPostgres 使用 PostgreSQL（需要引入 gorm.io/driver/postgres）
	DriverPostgres DriverType = "postgres"
	// DriverMySQL 使用 MySQL（需要引入 gorm.io/driver/mysql）
	DriverMySQL DriverType = "mysql"
)

// options 内部配置结构。
type options struct {
	dsn    string
	driver DriverType
	// dialector 允许外部直接注入 gorm.Dialector，绕过内置 driver 选择逻辑。
	// 适用于需要使用 glebarez/sqlite 等纯 Go 驱动的场景。
	dialector interface{ Name() string } //nolint:unused // gorm.Dialector
}

// defaultOptions 返回默认配置（SQLite，文件路径 bot.db）。
func defaultOptions() *options {
	return &options{
		dsn:    "bot.db",
		driver: DriverSQLite,
	}
}

// Option 函数式选项。
type Option func(*options)

// WithDSN 设置数据源名称（DSN）。
//
// SQLite 示例：
//
//	storage.WithDSN("data/bot.db")         // 文件数据库
//	storage.WithDSN("file::memory:?cache=shared")  // 内存数据库（测试）
//
// PostgreSQL 示例：
//
//	storage.WithDSN("host=localhost user=bot dbname=bot password=secret sslmode=disable")
func WithDSN(dsn string) Option {
	return func(o *options) { o.dsn = dsn }
}

// WithDriver 设置数据库驱动类型。
// 注意：使用非 SQLite 驱动时需要在 main 包中额外 import 对应的 gorm driver 包。
func WithDriver(driver DriverType) Option {
	return func(o *options) { o.driver = driver }
}
