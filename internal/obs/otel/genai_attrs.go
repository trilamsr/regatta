package otel

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// GenAI semconv attribute keys re-exported from semconv/v1.41.0. Spec
// docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md §8 seam contract
// bullet 4 pins this file as the single source of truth so the stream-json
// parser and any future direct-SDK call site emit the same keys without
// drifting on a version bump. Bumping the semconv package version is the
// only change a future SDK migration needs (§9 R4 encapsulation).
//
// GenAIInputMessages and GenAIOutputMessages are exported solely so the
// runtime regression guard TestGenAI_SensitivePayloadNotEmitted can name
// the forbidden keys explicitly — no parser code is permitted to set them
// (spec §2 Out-of-scope sensitive-payload policy, §9 R7).
var (
	GenAIOperationName             = semconv.GenAIOperationNameKey
	GenAIProviderName              = semconv.GenAIProviderNameKey
	GenAIRequestModel              = semconv.GenAIRequestModelKey
	GenAIRequestMaxTokens          = semconv.GenAIRequestMaxTokensKey
	GenAIResponseID                = semconv.GenAIResponseIDKey
	GenAIResponseModel             = semconv.GenAIResponseModelKey
	GenAIResponseFinishReasons     = semconv.GenAIResponseFinishReasonsKey
	GenAIUsageInputTokens          = semconv.GenAIUsageInputTokensKey
	GenAIUsageOutputTokens         = semconv.GenAIUsageOutputTokensKey
	GenAIUsageCacheReadInputTokens = semconv.GenAIUsageCacheReadInputTokensKey
	GenAIConversationID            = semconv.GenAIConversationIDKey
	GenAIInputMessages             = semconv.GenAIInputMessagesKey
	GenAIOutputMessages            = semconv.GenAIOutputMessagesKey
	ErrorType                      = semconv.ErrorTypeKey
)

// GenAI operation + provider constants — values for the two required
// attributes. Spec §3.4 pins "chat" + "anthropic" because regatta only
// observes the claude CLI subprocess; both are constants today.
const (
	GenAIOperationChat     = "chat"
	GenAIProviderAnthropic = "anthropic"
)

// ErrorTypeAttr emits the standard OTel error.type attribute. Helper
// exists so callers do not import semconv directly outside this package.
func ErrorTypeAttr(value string) attribute.KeyValue {
	return ErrorType.String(value)
}
