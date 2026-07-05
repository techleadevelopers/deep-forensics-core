package orchestrator

import "strings"

const (
	PlanFree       = "free"
	PlanStarter    = "starter"
	PlanPro        = "pro"
	PlanBusiness   = "business"
	PlanEnterprise = "enterprise"
	PlanAPI        = "api"

	ProfileLight = "light"
	ProfileFull  = "full"

	SubjectPaidHigh   = "verify.paid_high"
	SubjectPaidNorm   = "verify.paid_normal"
	SubjectFreeLow    = "verify.free_low"
	SubjectRetry      = "verify.retry"
	SubjectDeadLetter = "verify.dlq"
)

// NormalizePlan keeps plan names stable even when they come from headers,
// tenant records, or future billing integrations.
func NormalizePlan(plan string) string {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case PlanStarter:
		return PlanStarter
	case PlanPro:
		return PlanPro
	case PlanBusiness:
		return PlanBusiness
	case PlanEnterprise:
		return PlanEnterprise
	case PlanAPI:
		return PlanAPI
	default:
		return PlanFree
	}
}

func PipelineProfile(plan string) string {
	switch NormalizePlan(plan) {
	case PlanPro, PlanBusiness, PlanEnterprise, PlanAPI:
		return ProfileFull
	default:
		return ProfileLight
	}
}

func QueueSubject(plan string) string {
	switch NormalizePlan(plan) {
	case PlanEnterprise, PlanAPI:
		return SubjectPaidHigh
	case PlanPro, PlanBusiness:
		return SubjectPaidNorm
	default:
		return SubjectFreeLow
	}
}
