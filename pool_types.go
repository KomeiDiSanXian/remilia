package remilia

import "github.com/KomeiDiSanXian/remilia/infra/pool"

// Re-export pool types for backward compatibility.

type Pool = pool.Pool

type PoolStats = pool.PoolStats

type InstrumentedPool = pool.InstrumentedPool
