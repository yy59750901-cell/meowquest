package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTranscribeWaitsBeyondOldFifteenSecondLimit(t *testing.T) {
	var polls atomic.Int32
	var base string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tasks/existing" {
			if polls.Add(1) <= 15 {
				json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_status": "RUNNING"}})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_status": "SUCCEEDED", "results": []map[string]string{{"transcription_url": base + "/transcript"}}}})
			}
		} else if r.URL.Path == "/transcript" {
			json.NewEncoder(w).Encode(map[string]any{"transcripts": []map[string]string{{"text": "超过十五秒仍完整等待"}, {"text": "第二段"}}})
		} else {
			t.Error("缓存的 task ID 不应重新提交")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer mock.Close()
	base = mock.URL
	t.Setenv("MEOW_ASR_BASE_URL", base)
	client := &Client{APIKey: "test"}
	state := &TranscriptionState{TaskID: "existing"}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	text, err := client.TranscribeCached(ctx, "unused", state, nil)
	if err != nil || polls.Load() != 16 || text != "超过十五秒仍完整等待\n第二段" || !state.Done {
		t.Fatalf("旧 15 秒上限或转写不完整: polls=%d text=%q state=%+v err=%v", polls.Load(), text, state, err)
	}
}

func TestTranscribeTimeoutKeepsUpstreamTaskAndFetchUsesContext(t *testing.T) {
	var submits atomic.Int32
	var saves atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/transcription") {
			submits.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_id": "resume-me"}})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_status": "RUNNING"}})
		}
	}))
	defer mock.Close()
	t.Setenv("MEOW_ASR_BASE_URL", mock.URL)
	client := &Client{APIKey: "test"}
	state := &TranscriptionState{}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := client.TranscribeCached(ctx, "https://example.invalid/audio.wav", state, func() error { saves.Add(1); return nil })
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("没有遵守 context: %v", err)
		}
	}
	if submits.Load() != 1 || saves.Load() != 1 || state.TaskID != "resume-me" || state.Done {
		t.Fatalf("超时重试丢缓存/重复提交: %+v submits=%d saves=%d", state, submits.Load(), saves.Load())
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	fetchMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer fetchMock.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := fetchTranscript(ctx, fetchMock.URL); result <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("没有开始拉转写结果")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("fetchTranscript 忽略 context: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fetchTranscript 无法被取消")
	}
}

func TestTranscribeCachedFetchFailureDoesNotResubmitOrRepoll(t *testing.T) {
	var fetches atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/saved-transcript" {
			t.Error("缓存的结果 URL 不应重新提交或轮询")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if fetches.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"transcripts": []map[string]string{{"text": "完整结果"}}})
	}))
	defer mock.Close()
	t.Setenv("MEOW_ASR_BASE_URL", mock.URL)
	client := &Client{APIKey: "test"}
	state := &TranscriptionState{TaskID: "saved", TranscriptURL: mock.URL + "/saved-transcript"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.TranscribeCached(ctx, "unused", state, nil); err == nil {
		t.Fatal("拉取失败被静默吞掉")
	}
	text, err := client.TranscribeCached(ctx, "unused", state, nil)
	if err != nil || text != "完整结果" || fetches.Load() != 2 || !state.Done {
		t.Fatalf("结果拉取重试失败: %q %+v %v", text, state, err)
	}
}
