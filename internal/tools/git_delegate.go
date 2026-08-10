package tools

import (
	"context"

	"github.com/orchestra/orchestra/internal/tools/git"
)

func (r *Runner) gitClient() *git.Client {
	if r == nil {
		return nil
	}
	return git.NewClient(r.workspaceRoot)
}

func (r *Runner) GitStatus(ctx context.Context, req GitStatusRequest) (*GitStatusResponse, error) {
	return r.gitClient().GitStatus(ctx, req)
}

func (r *Runner) GitLog(ctx context.Context, req GitLogRequest) (*GitLogResponse, error) {
	return r.gitClient().GitLog(ctx, req)
}

func (r *Runner) GitDiff(ctx context.Context, req GitDiffRequest) (*GitDiffResponse, error) {
	return r.gitClient().GitDiff(ctx, req)
}

func (r *Runner) GitCommit(ctx context.Context, req GitCommitRequest) (*GitCommitResponse, error) {
	return r.gitClient().GitCommit(ctx, req)
}

func (r *Runner) GitBranch(ctx context.Context, req GitBranchRequest) (*GitBranchResponse, error) {
	return r.gitClient().GitBranch(ctx, req)
}

func (r *Runner) GitCheckout(ctx context.Context, req GitCheckoutRequest) (*GitCheckoutResponse, error) {
	return r.gitClient().GitCheckout(ctx, req)
}

func (r *Runner) GitPush(ctx context.Context, req GitPushRequest) (*GitPushResponse, error) {
	return r.gitClient().GitPush(ctx, req)
}

func (r *Runner) GitWorktreeList(ctx context.Context, req GitWorktreeListRequest) (*GitWorktreeListResponse, error) {
	return r.gitClient().GitWorktreeList(ctx, req)
}

func (r *Runner) GitWorktreeAdd(ctx context.Context, req GitWorktreeAddRequest) (*GitWorktreeAddResponse, error) {
	return r.gitClient().GitWorktreeAdd(ctx, req)
}

func (r *Runner) GitWorktreeRemove(ctx context.Context, req GitWorktreeRemoveRequest) (*GitWorktreeRemoveResponse, error) {
	return r.gitClient().GitWorktreeRemove(ctx, req)
}

func (r *Runner) GitWorktreePrune(ctx context.Context, req GitWorktreePruneRequest) (*GitWorktreePruneResponse, error) {
	return r.gitClient().GitWorktreePrune(ctx, req)
}

func (r *Runner) GHPRList(ctx context.Context, req GHPRListRequest) (*GHPRListResponse, error) {
	return r.gitClient().GHPRList(ctx, req)
}

func (r *Runner) GHPRCreate(ctx context.Context, req GHPRCreateRequest) (*GHPRCreateResponse, error) {
	return r.gitClient().GHPRCreate(ctx, req)
}

func (r *Runner) GHPRView(ctx context.Context, req GHPRViewRequest) (*GHPRViewResponse, error) {
	return r.gitClient().GHPRView(ctx, req)
}

func (r *Runner) GHIssueList(ctx context.Context, req GHIssueListRequest) (*GHIssueListResponse, error) {
	return r.gitClient().GHIssueList(ctx, req)
}

func (r *Runner) GHIssueView(ctx context.Context, req GHIssueViewRequest) (*GHIssueViewResponse, error) {
	return r.gitClient().GHIssueView(ctx, req)
}
