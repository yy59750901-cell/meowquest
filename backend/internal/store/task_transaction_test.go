package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 仅模拟database/sql的BeginTx合同，不连接PG/MySQL服务器。
type taskTxRecorder struct {
	gorm.ConnPool
	options  sql.TxOptions
	begins   int
	beginErr error
	tx       *recordedTaskTx
}

func (r *taskTxRecorder) BeginTx(_ context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	r.begins++
	if options != nil {
		r.options = *options
	}
	return r.tx, r.beginErr
}

type recordedTaskTx struct {
	gorm.ConnPool
	committed  bool
	rolledBack bool
}

func (t *recordedTaskTx) Commit() error   { t.committed = true; return nil }
func (t *recordedTaskTx) Rollback() error { t.rolledBack = true; return nil }

func taskRecorderDB(t *testing.T) (*DB, *taskTxRecorder) {
	t.Helper()
	recorder := &taskTxRecorder{tx: &recordedTaskTx{}}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: recorder}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return &DB{DB: db}, recorder
}

func TestTaskTransactionPassesReadCommittedOptionsAndRejectsNested(t *testing.T) {
	db, recorder := taskRecorderDB(t)
	err := db.TaskTransaction(func(tx *gorm.DB) error {
		child := &DB{DB: tx}
		if err := child.RequireTaskTransaction(); err != nil {
			return err
		}
		if err := child.TaskTransaction(func(*gorm.DB) error { t.Fatal("嵌套任务事务执行了"); return nil }); err == nil {
			t.Fatal("嵌套事务未被拒绝")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if recorder.begins != 1 || recorder.options.Isolation != sql.LevelReadCommitted || recorder.options.ReadOnly || !recorder.tx.committed {
		t.Fatalf("未显式向BeginTx传RC: %+v", recorder)
	}
	if err := db.RequireTaskTransaction(); err == nil {
		t.Fatal("裸DB被允许进入任务内核")
	}
}

func TestTaskTransactionPropagatesBeginAndBodyErrors(t *testing.T) {
	for _, atBegin := range []bool{false, true} {
		t.Run(map[bool]string{false: "事务内错误", true: "启动错误"}[atBegin], func(t *testing.T) {
			db, recorder := taskRecorderDB(t)
			expected := errors.New("模拟事务失败")
			if atBegin {
				recorder.beginErr = expected
			}
			entered := false
			err := db.TaskTransaction(func(*gorm.DB) error { entered = true; return expected })
			if !errors.Is(err, expected) || recorder.tx.committed {
				t.Fatalf("事务错误被吞或错误提交: %v", err)
			}
			if atBegin && entered {
				t.Fatal("Begin失败还进入了回调")
			}
			if !atBegin && !recorder.tx.rolledBack {
				t.Fatal("回调失败未回滚")
			}
		})
	}
}
