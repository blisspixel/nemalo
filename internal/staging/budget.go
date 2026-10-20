package staging

import (
	"context"
	"errors"
	"io"
	"sync"
)

var errTreeBudget = errors.New("the archive tree's shared work budget was spent; original sources are preserved")

// allowance also spans the concurrent stdout/stderr copies in os/exec. A
// rejected request exhausts it, so later nodes cannot repeat the overrun.
type allowance struct {
	mu        sync.Mutex
	remaining int64
}

func (a *allowance) take(n int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n < 0 || n > a.remaining {
		a.remaining = 0
		return errTreeBudget
	}
	a.remaining -= n
	return nil
}

func (a *allowance) left() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.remaining
}

type budgetWriter struct {
	w io.Writer
	a *allowance
}

func (w budgetWriter) Write(p []byte) (int, error) {
	if err := w.a.take(int64(len(p))); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

type budgetReaderAt struct {
	r io.ReaderAt
	a *allowance
}

func (r budgetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.a.take(int64(len(p))); err != nil {
		return 0, err
	}
	return r.r.ReadAt(p, off)
}

type treeBudget struct {
	members, processes int
	bytes              int64
	reads, diagnostics allowance
	proofs             allowance
}

func (l Limits) memberLimit() int {
	if l.tree != nil {
		return min(l.Members, l.tree.members)
	}
	return l.Members
}

func (l Limits) countMembers(n int) error {
	if n > l.memberLimit() {
		if l.tree != nil {
			l.tree.members = 0
		}
		return errTreeBudget
	}
	if l.tree != nil {
		l.tree.members -= n
	}
	return nil
}

func (l Limits) reserveMember(bytes int64) error {
	if l.tree != nil {
		if bytes < 0 || bytes >= l.tree.bytes {
			return errTreeBudget
		}
		// Include the byte that detects a lying size, even for failed copies.
		l.tree.bytes -= bytes + 1
	}
	return nil
}

func (l Limits) reader(r io.ReaderAt) io.ReaderAt {
	if l.tree == nil {
		return r
	}
	return budgetReaderAt{r, &l.tree.reads}
}

// Coverage work is bounded once for the entire tree, and is also part of
// the tree's existing reader-work allowance. Failed requests stay charged.
func (l Limits) proofReader(r io.ReaderAt) io.ReaderAt {
	if l.tree == nil {
		return r
	}
	return budgetReaderAt{l.reader(r), &l.tree.proofs}
}

func (l Limits) run(ctx context.Context, exe string, out io.Writer, limit int64, args ...string) ([]byte, int, error) {
	if l.tree == nil {
		return runSevenLimit(ctx, exe, out, limit, args...)
	}
	if err := ctx.Err(); err != nil {
		return nil, -1, err
	}
	if l.tree.processes <= 0 || l.tree.diagnostics.left() == 0 {
		return nil, -1, errTreeBudget
	}
	l.tree.processes--
	return runSevenBounded(ctx, exe, out, limit, &l.tree.diagnostics, args...)
}
