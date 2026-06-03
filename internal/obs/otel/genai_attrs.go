package otel

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// GenAI semconv keys re-exported from semconv/v1.41.0. Spec §8 seam
// bullet 4 pins this file as single source of truth so the stream-json
// parser + future SDK callers emit the same keys without drifting on
// a version bump (§9 R4). GenAIInputMessages/OutputMessages are
// exported only so TestGenAI_SensitivePayloadNotEmitted can name the
// forbidden keys — parser code MUST NOT set them (spec §2 + §9 R7).
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

// Operation + provider constants. Spec §3.4 pins these because
// regatta only observes the claude CLI subprocess; both are constants.
const (
	GenAIOperationChat     = "chat"
	GenAIProviderAnthropic = "anthropic"
)

// ErrorTypeAttr emits the standard OTel error.type attr so callers
// do not import semconv outside this package.
func ErrorTypeAttr(value string) attribute.KeyValue {
	return ErrorType.String(value)
}
