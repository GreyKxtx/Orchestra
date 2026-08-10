package tools

import (
	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/internal/tools/git"
	"github.com/orchestra/orchestra/internal/tools/nav"
	"github.com/orchestra/orchestra/internal/tools/session"
	"github.com/orchestra/orchestra/internal/tools/task"
)

// Exec types (backward compat).
type (
	ExecRunRequest             = exec.RunRequest
	ExecRunResponse            = exec.RunResponse
	ExecBashBackgroundRequest  = exec.BashBackgroundRequest
	ExecBashBackgroundResponse = exec.BashBackgroundResponse
	ExecBashOutputRequest      = exec.BashOutputRequest
	ExecBashOutputResponse     = exec.BashOutputResponse
	ExecBashKillRequest        = exec.BashKillRequest
	ExecBashKillResponse       = exec.BashKillResponse
)

// WithExecOutputCallback re-exports exec.WithOutputCallback.
var WithExecOutputCallback = exec.WithOutputCallback

// Git types (backward compat).
type (
	GitStatusRequest    = git.GitStatusRequest
	GitStatusResponse   = git.GitStatusResponse
	GitLogRequest       = git.GitLogRequest
	GitLogResponse      = git.GitLogResponse
	GitDiffRequest      = git.GitDiffRequest
	GitDiffResponse     = git.GitDiffResponse
	GitCommitRequest    = git.GitCommitRequest
	GitCommitResponse   = git.GitCommitResponse
	GitBranchRequest    = git.GitBranchRequest
	GitBranchResponse   = git.GitBranchResponse
	GitCheckoutRequest  = git.GitCheckoutRequest
	GitCheckoutResponse = git.GitCheckoutResponse
	GitPushRequest      = git.GitPushRequest
	GitPushResponse     = git.GitPushResponse
	GitWorktreeListRequest   = git.GitWorktreeListRequest
	GitWorktreeListResponse  = git.GitWorktreeListResponse
	GitWorktreeAddRequest    = git.GitWorktreeAddRequest
	GitWorktreeAddResponse   = git.GitWorktreeAddResponse
	GitWorktreeRemoveRequest = git.GitWorktreeRemoveRequest
	GitWorktreeRemoveResponse = git.GitWorktreeRemoveResponse
	GitWorktreePruneRequest  = git.GitWorktreePruneRequest
	GitWorktreePruneResponse = git.GitWorktreePruneResponse
	GHPRListRequest     = git.GHPRListRequest
	GHPRListResponse    = git.GHPRListResponse
	GHPRListItem        = git.GHPRListItem
	GHPRCreateRequest   = git.GHPRCreateRequest
	GHPRCreateResponse  = git.GHPRCreateResponse
	GHPRViewRequest     = git.GHPRViewRequest
	GHPRViewResponse    = git.GHPRViewResponse
	GHPRComment         = git.GHPRComment
	GHIssueListRequest  = git.GHIssueListRequest
	GHIssueListResponse = git.GHIssueListResponse
	GHIssueListItem     = git.GHIssueListItem
	GHIssueViewRequest  = git.GHIssueViewRequest
	GHIssueViewResponse = git.GHIssueViewResponse
	GHIssueComment      = git.GHIssueComment
)

// FS types (backward compat).
type (
	FSListRequest      = fs.FSListRequest
	FSListResponse     = fs.FSListResponse
	FSFileMeta         = fs.FSFileMeta
	FSReadRequest      = fs.FSReadRequest
	FSReadResponse     = fs.FSReadResponse
	FSApplyOpsRequest  = fs.FSApplyOpsRequest
	FSApplyOpsResponse = fs.FSApplyOpsResponse
	FSGlobRequest      = fs.FSGlobRequest
	FSGlobResponse     = fs.FSGlobResponse
	FSWriteRequest     = fs.FSWriteRequest
	FSWriteResponse    = fs.FSWriteResponse
	FSEditRequest      = fs.FSEditRequest
	FSEditResponse     = fs.FSEditResponse
	FSDeleteRequest    = fs.FSDeleteRequest
	FSDeleteResponse   = fs.FSDeleteResponse
	FSRenameRequest    = fs.FSRenameRequest
	FSRenameResponse   = fs.FSRenameResponse
	FSPreviewRequest   = fs.FSPreviewRequest
	FSPreviewResponse  = fs.FSPreviewResponse
	ASTRenameRequest   = fs.ASTRenameRequest
	ASTRenameResponse  = fs.ASTRenameResponse
	SearchTextRequest  = fs.SearchTextRequest
	SearchTextOptions  = fs.SearchTextOptions
	SearchTextMatch    = fs.SearchTextMatch
	SearchTextResponse = fs.SearchTextResponse
)

// ToolASTRename re-exports fs tool def for backward compat.
var ToolASTRename = fs.ToolASTRename

// Nav types (backward compat).
type (
	CodeSymbolsRequest     = nav.CodeSymbolsRequest
	CodeSymbolsResponse    = nav.CodeSymbolsResponse
	Symbol                 = nav.Symbol
	ExploreCodebaseRequest = nav.ExploreCodebaseRequest
	ExploreCodebaseResponse = nav.ExploreCodebaseResponse
	SemanticSearchRequest  = nav.SemanticSearchRequest
	SemanticSearchHit      = nav.SemanticSearchHit
	SemanticSearchResponse = nav.SemanticSearchResponse
	RepoMapRequest         = nav.RepoMapRequest
	RepoMapResponse        = nav.RepoMapResponse
	CKGIndexView           = nav.CKGIndexView
	CKGEmbedResult         = nav.CKGEmbedResult
)

// Nav tool defs (backward compat).
var (
	ToolSemanticSearch = nav.ToolSemanticSearch
	ToolRepoMap        = nav.ToolRepoMap
)

// Task tool defs (backward compat).
var (
	ToolSkillInvoke = task.ToolSkillInvoke
	ToolTaskResult  = task.ToolTaskResult
)

// Session types (backward compat).
type (
	TodoStatus           = session.TodoStatus
	TodoItem             = session.TodoItem
	TodoWriteRequest     = session.TodoWriteRequest
	TodoWriteResponse    = session.TodoWriteResponse
	TodoReadResponse     = session.TodoReadResponse
	QuestionItem         = session.QuestionItem
	QuestionAsker        = session.QuestionAsker
	StdinQuestionAsker   = session.StdinQuestionAsker
	MemoryWriteRequest   = session.MemoryWriteRequest
	MemoryWriteResponse  = session.MemoryWriteResponse
	MemoryReadRequest    = session.MemoryReadRequest
	MemoryReadResponse   = session.MemoryReadResponse
	MemorySearchRequest  = session.MemorySearchRequest
	MemorySearchHit      = session.MemorySearchHit
	MemorySearchResponse = session.MemorySearchResponse
	RuntimeQueryRequest  = session.RuntimeQueryRequest
	RuntimeQueryResponse = session.RuntimeQueryResponse
	RuntimeSpanResult    = session.RuntimeSpanResult
)

const (
	TodoPending    = session.TodoPending
	TodoInProgress = session.TodoInProgress
	TodoDone       = session.TodoDone
	TodoCancelled  = session.TodoCancelled
)

// ValidateTodos re-exports session todo validation.
var ValidateTodos = session.ValidateTodos
