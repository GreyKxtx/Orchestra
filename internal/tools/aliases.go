package tools

import (
	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/internal/tools/git"
)

// Exec types (backward compat).
type (
	ExecRunRequest            = exec.RunRequest
	ExecRunResponse           = exec.RunResponse
	ExecBashBackgroundRequest = exec.BashBackgroundRequest
	ExecBashBackgroundResponse = exec.BashBackgroundResponse
	ExecBashOutputRequest     = exec.BashOutputRequest
	ExecBashOutputResponse    = exec.BashOutputResponse
	ExecBashKillRequest       = exec.BashKillRequest
	ExecBashKillResponse      = exec.BashKillResponse
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
	FSListRequest       = fs.FSListRequest
	FSListResponse      = fs.FSListResponse
	FSFileMeta          = fs.FSFileMeta
	FSReadRequest       = fs.FSReadRequest
	FSReadResponse      = fs.FSReadResponse
	FSApplyOpsRequest   = fs.FSApplyOpsRequest
	FSApplyOpsResponse  = fs.FSApplyOpsResponse
	FSGlobRequest       = fs.FSGlobRequest
	FSGlobResponse      = fs.FSGlobResponse
	FSWriteRequest      = fs.FSWriteRequest
	FSWriteResponse     = fs.FSWriteResponse
	FSEditRequest       = fs.FSEditRequest
	FSEditResponse      = fs.FSEditResponse
	FSDeleteRequest     = fs.FSDeleteRequest
	FSDeleteResponse    = fs.FSDeleteResponse
	FSRenameRequest     = fs.FSRenameRequest
	FSRenameResponse    = fs.FSRenameResponse
	FSPreviewRequest    = fs.FSPreviewRequest
	FSPreviewResponse   = fs.FSPreviewResponse
	ASTRenameRequest    = fs.ASTRenameRequest
	ASTRenameResponse   = fs.ASTRenameResponse
	SearchTextRequest   = fs.SearchTextRequest
	SearchTextOptions   = fs.SearchTextOptions
	SearchTextMatch     = fs.SearchTextMatch
	SearchTextResponse  = fs.SearchTextResponse
)

// ToolASTRename re-exports fs tool def for backward compat.
var ToolASTRename = fs.ToolASTRename
