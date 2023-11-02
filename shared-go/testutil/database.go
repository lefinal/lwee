package testutil

import (
	"context"
	"errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"sync"
)

// DBTxSupplier supplies with pgx.Tx and serves an implementation of
// pgutil.DBTxSupplier for testing purposes.
type DBTxSupplier struct {
	// BeginFail describes whether calls to Begin should fail.
	BeginFail bool
	// GenTx describes whether transactions should be generated automatically when
	// required.
	GenTx bool
	// Tx are the DBTx to return in calls to Begin.
	Tx []*DBTx
	// txOffset keeps track of the current tx in Tx.
	txOffset int
	// txOffsetMutex locks txOffset.
	txOffsetMutex sync.Mutex
}

// Begin return the next Tx if BeginFail is not set.
func (supplier *DBTxSupplier) Begin(_ context.Context) (pgx.Tx, error) {
	if supplier.BeginFail {
		return nil, errors.New("begin fail")
	}
	supplier.txOffsetMutex.Lock()
	defer supplier.txOffsetMutex.Unlock()
	if supplier.txOffset >= len(supplier.Tx) {
		if supplier.GenTx {
			return &DBTx{}, nil
		}
		panic("out of tx")
	}
	tx := supplier.Tx[supplier.txOffset]
	supplier.txOffset++
	return tx, nil
}

// DBTx is a simplified implemenation of pgx.Tx for testing purposes.
type DBTx struct {
	// IsCommitted describes whether the tx was committed using Commit.
	IsCommitted bool
	// CommitFail describes whether calls to Commit should fail.
	CommitFail bool
	// RollbackFail describes whether calls to Rollback should fail.
	RollbackFail bool
	// QueryAndExecFail describes whether calls to Query and exec should fail.
	QueryAndExecFail bool
	// CommandTags to return in calls to Exec when not failing.
	CommandTags []pgconn.CommandTag
	// Rows to return in calls to Query if QueryAndExecFail is not set.
	Rows []pgx.Rows
}

// Begin is currently unsupported.
func (tx *DBTx) Begin(_ context.Context) (pgx.Tx, error) {
	panic("unsupported")
}

// BeginFunc is currently unsupported.
func (tx *DBTx) BeginFunc(_ context.Context, _ func(pgx.Tx) error) error {
	panic("unsupported")
}

// Commit fails if CommitFail is set.
func (tx *DBTx) Commit(_ context.Context) error {
	if tx.CommitFail {
		return errors.New("commit fail")
	}
	tx.IsCommitted = true
	return nil
}

// Rollback fails if RollbackFail is set.
func (tx *DBTx) Rollback(_ context.Context) error {
	if tx.RollbackFail {
		return errors.New("rollback fail")
	}
	return nil
}

// CopyFrom is currently unsupported.
func (tx *DBTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("unsupported")
}

// SendBatch is currently unsupported.
func (tx *DBTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("unsupported")
}

// LargeObjects is currently unsupported.
func (tx *DBTx) LargeObjects() pgx.LargeObjects {
	panic("unsupported")
}

// Prepare is currently unsupported.
func (tx *DBTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("unsupported")
}

// Exec returns the next from CommandTags if QueryAndExecFail is not set.
func (tx *DBTx) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	if tx.QueryAndExecFail {
		return nil, errors.New("exec fail")
	}
	if len(tx.CommandTags) == 0 {
		panic("out of command tags")
	}
	var commandTag pgconn.CommandTag
	commandTag, tx.CommandTags = tx.CommandTags[0], tx.CommandTags[1:]
	return commandTag, nil
}

// Query returns the next from Rows if QueryAndExecFail is not set.
func (tx *DBTx) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	if tx.QueryAndExecFail {
		return nil, errors.New("query fail")
	}
	if len(tx.Rows) == 0 {
		panic("out of rows")
	}
	var rows pgx.Rows
	rows, tx.Rows = tx.Rows[0], tx.Rows[1:]
	return rows, nil
}

// QueryRow is currently unsupported.
func (tx *DBTx) QueryRow(_ context.Context, _ string, _ ...interface{}) pgx.Row {
	panic("unsupported")
}

// QueryFunc is currently unsupported.
func (tx *DBTx) QueryFunc(_ context.Context, _ string, _ []interface{}, _ []interface{},
	_ func(pgx.QueryFuncRow) error) (pgconn.CommandTag, error) {
	panic("unsuported")
}

// Conn is currently unsupported.
func (tx *DBTx) Conn() *pgx.Conn {
	panic("unsupported")
}
