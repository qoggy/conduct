// Package apperror 定义 CLI 与 HTTP API 共用的机器稳定领域错误。
package apperror

import (
	"errors"
	"fmt"
)

// Code 是不随界面语言变化的错误码。
type Code string

const (
	CodeTechnicalFailure            Code = "technical_failure"
	CodeWorkflowNotFound            Code = "workflow_not_found"
	CodeWorkflowAlreadyExists       Code = "workflow_already_exists"
	CodeRunNotFound                 Code = "run_not_found"
	CodeRunAlreadyExists            Code = "run_already_exists"
	CodeRunSummaryNotFound          Code = "run_summary_not_found"
	CodeWorkingDirectoryNotFound    Code = "working_directory_not_found"
	CodeWorkingDirectoryNotDir      Code = "working_directory_not_directory"
	CodeWorkflowValidationFailed    Code = "workflow_validation_failed"
	CodeRunNotStoppable             Code = "run_not_stoppable"
	CodeRunNotResumable             Code = "run_not_resumable"
	CodeRunNotDeletable             Code = "run_not_deletable"
	CodeWorkflowSaveConflict        Code = "workflow_save_conflict"
	CodeUserPromptRequired          Code = "user_prompt_required"
	CodeWorkingDirectoryMustBeAbs   Code = "working_directory_must_be_absolute"
	CodeNodeNotFound                Code = "node_not_found"
	CodeNodeAlreadyExists           Code = "node_already_exists"
	CodeReservedNodeID              Code = "reserved_node_id"
	CodeInvalidNodeID               Code = "invalid_node_id"
	CodeNodeDisplayNameRequired     Code = "node_display_name_required"
	CodeInvalidRequest              Code = "invalid_request"
	CodeInvalidSettingsRequest      Code = "invalid_settings_request"
	CodeUnsupportedMediaType        Code = "unsupported_media_type"
	CodeForbiddenOrigin             Code = "forbidden_origin"
	CodeDirectoryNotFound           Code = "directory_not_found"
	CodePathNotDirectory            Code = "path_not_directory"
	CodeEdgeNotFound                Code = "edge_not_found"
	CodeEdgeAlreadyExists           Code = "edge_already_exists"
	CodeWorkflowNameInvalidChars    Code = "workflow_name_invalid_characters"
	CodeWorkflowNameReserved        Code = "workflow_name_reserved"
	CodeRunIDInvalid                Code = "run_id_invalid"
	CodeNodesRequired               Code = "nodes_required"
	CodeDuplicateNodeID             Code = "duplicate_node_id"
	CodeStartNodeCount              Code = "start_node_count"
	CodeEndNodeCount                Code = "end_node_count"
	CodeAgentNodeRequired           Code = "agent_node_required"
	CodeMarkerFieldNotEmpty         Code = "marker_field_not_empty"
	CodeRequiredField               Code = "required_field"
	CodeEdgeEndpointsRequired       Code = "edge_endpoints_required"
	CodeEdgeFromNodeNotFound        Code = "edge_from_node_not_found"
	CodeEdgeToNodeNotFound          Code = "edge_to_node_not_found"
	CodeSelfEdge                    Code = "self_edge"
	CodeStartEndDirectEdge          Code = "start_end_direct_edge"
	CodeEdgeToStart                 Code = "edge_to_start"
	CodeEdgeFromEnd                 Code = "edge_from_end"
	CodeDuplicateEdge               Code = "duplicate_edge"
	CodeCycleDetected               Code = "cycle_detected"
	CodeNodeMissingIncomingEdge     Code = "node_missing_incoming_edge"
	CodeNodeMissingOutgoingEdge     Code = "node_missing_outgoing_edge"
	CodeUnknownSystemVariable       Code = "unknown_system_variable"
	CodeMarkerNodeReference         Code = "marker_node_reference"
	CodeNonAncestorNodeReference    Code = "non_ancestor_node_reference"
	CodeNodeReferenceNotFound       Code = "node_reference_not_found"
	CodeUnknownEngine               Code = "unknown_engine"
	CodeEngineModelNotAllowed       Code = "engine_model_not_allowed"
	CodeEngineEffortFieldNotAllowed Code = "engine_effort_field_not_allowed"
	CodeEngineEffortValueNotAllowed Code = "engine_effort_value_not_allowed"
)

// AllCodes 返回所有需要由 CLI 和 UI 渲染的稳定错误码。
func AllCodes() []Code {
	return []Code{
		CodeTechnicalFailure, CodeWorkflowNotFound, CodeWorkflowAlreadyExists, CodeRunNotFound,
		CodeRunAlreadyExists, CodeRunSummaryNotFound, CodeWorkingDirectoryNotFound,
		CodeWorkingDirectoryNotDir, CodeWorkflowValidationFailed, CodeRunNotStoppable,
		CodeRunNotResumable, CodeRunNotDeletable, CodeWorkflowSaveConflict, CodeUserPromptRequired,
		CodeWorkingDirectoryMustBeAbs, CodeNodeNotFound, CodeNodeAlreadyExists, CodeReservedNodeID,
		CodeInvalidNodeID, CodeNodeDisplayNameRequired, CodeInvalidRequest, CodeInvalidSettingsRequest,
		CodeUnsupportedMediaType, CodeForbiddenOrigin, CodeDirectoryNotFound, CodePathNotDirectory,
		CodeEdgeNotFound, CodeEdgeAlreadyExists, CodeWorkflowNameInvalidChars, CodeWorkflowNameReserved,
		CodeRunIDInvalid, CodeNodesRequired, CodeDuplicateNodeID, CodeStartNodeCount, CodeEndNodeCount,
		CodeAgentNodeRequired, CodeMarkerFieldNotEmpty, CodeRequiredField, CodeEdgeEndpointsRequired,
		CodeEdgeFromNodeNotFound, CodeEdgeToNodeNotFound, CodeSelfEdge, CodeStartEndDirectEdge,
		CodeEdgeToStart, CodeEdgeFromEnd, CodeDuplicateEdge, CodeCycleDetected,
		CodeNodeMissingIncomingEdge, CodeNodeMissingOutgoingEdge, CodeUnknownSystemVariable,
		CodeMarkerNodeReference, CodeNonAncestorNodeReference, CodeNodeReferenceNotFound,
		CodeUnknownEngine, CodeEngineModelNotAllowed,
		CodeEngineEffortFieldNotAllowed, CodeEngineEffortValueNotAllowed,
	}
}

// Params 是错误模板的结构化参数。值仅使用 JSON 可编码的标量或切片。
type Params map[string]any

// Problem 是批量校验中的一条字段错误。
type Problem struct {
	Path   string `json:"path"`
	Code   Code   `json:"code"`
	Params Params `json:"params,omitempty"`
}

// Error 是领域错误或带技术详情的技术错误。
type Error struct {
	Code            Code      `json:"code"`
	Params          Params    `json:"params,omitempty"`
	Problems        []Problem `json:"problems,omitempty"`
	TechnicalDetail string    `json:"technicalDetail,omitempty"`
	cause           error
}

func (e *Error) Error() string {
	if e.TechnicalDetail != "" {
		return e.TechnicalDetail
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.cause }

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e.Code == other.Code
}

func New(code Code, params Params) *Error {
	return &Error{Code: code, Params: params}
}

func Validation(problems []Problem) *Error {
	return &Error{Code: CodeWorkflowValidationFailed, Problems: problems}
}

func Technical(detail string, cause error) *Error {
	return &Error{Code: CodeTechnicalFailure, TechnicalDetail: detail, cause: cause}
}

func Technicalf(cause error, format string, arguments ...any) *Error {
	return Technical(fmt.Sprintf(format, arguments...), cause)
}

// As 返回错误链中的结构化错误。
func As(err error) (*Error, bool) {
	var applicationError *Error
	ok := errors.As(err, &applicationError)
	return applicationError, ok
}

// CodeOf 返回错误链中的稳定错误码。
func CodeOf(err error) (Code, bool) {
	applicationError, ok := As(err)
	if !ok {
		return "", false
	}
	return applicationError.Code, true
}
