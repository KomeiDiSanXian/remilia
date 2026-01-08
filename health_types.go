package remilia

import "github.com/KomeiDiSanXian/remilia/infra/health"

// Re-export health types for backward compatibility.

type HealthStatus = health.HealthStatus

const (
	HealthStatusHealthy   = health.HealthStatusHealthy
	HealthStatusUnhealthy = health.HealthStatusUnhealthy
	HealthStatusDegraded  = health.HealthStatusDegraded
)

type HealthChecker = health.HealthChecker

type HealthCheckResult = health.HealthCheckResult

type HealthCheckResponse = health.HealthCheckResponse

type HealthCheck = health.HealthCheck
