/*
  MIT License

  Copyright (c) 2018 Star Brilliant

  Permission is hereby granted, free of charge, to any person obtaining a copy
  of this software and associated documentation files (the "Software"), to deal
  in the Software without restriction, including without limitation the rights
  to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
  copies of the Software, and to permit persons to whom the Software is
  furnished to do so, subject to the following conditions:

  The above copyright notice and this permission notice shall be included in
  all copies or substantial portions of the Software.

  THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
  IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
  FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
  AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
  LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
  OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
  SOFTWARE.
*/

package actionqueue

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	q := New()
	q.Run(ctx)
	cancel()
	for q.runningState != stateStopped {
		runtime.Gosched()
	}
}

func TestStop(t *testing.T) {
	q := New()
	q.Run(context.Background())
	q.Stop()
	if q.runningState != stateStopped {
		t.Fail()
	}
}

func TestAddAction1(t *testing.T) {
	q := New()
	q.Run(context.Background())
	q.AddAction(42, time.Time{})
	action := <-q.NextAction()
	if action.Value != 42 {
		t.Fail()
	}
	q.Stop()
}

func TestAddAction2(t *testing.T) {
	q := New()
	q.Run(context.Background())
	q.AddAction(44, time.Now())
	q.AddAction(42, time.Time{})
	q.AddAction(43, time.Time{})
	action := <-q.NextAction()
	if action.Value != 42 {
		t.Fail()
	}
	action = <-q.NextAction()
	if action.Value != 43 {
		t.Fail()
	}
	action = <-q.NextAction()
	if action.Value != 44 {
		t.Fail()
	}
	q.Stop()
}

func TestAddActionWithExpiry1(t *testing.T) {
	q := New()
	q.Run(context.Background())
	q.AddActionWithExpiry(42, time.Time{}, time.Now().Add(-time.Second))
	for len(q.pushActionChan) != 0 {
		runtime.Gosched()
	}
	q.Stop()
	if len(q.actions) != 0 {
		t.Fail()
	}
}

func TestAddActionWithExpiry2(t *testing.T) {
	q := New()
	q.Run(context.Background())
	q.AddActionWithExpiry(42, time.Time{}, time.Now().Add(time.Hour))
	action := <-q.NextAction()
	if action.Value != 42 {
		t.Fail()
	}
	q.Stop()
}

func TestDump(t *testing.T) {
	q := New()
	q.Run(context.Background())
	q.AddAction(42, time.Time{})
	for len(q.pushActionChan) != 0 {
		runtime.Gosched()
	}
	q.Stop()
	actions := q.Dump()
	if len(actions) != 1 || actions[0].Value != 42 {
		t.Fail()
	}
}

func TestDumpPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fail()
		}
	}()
	q := New()
	atomic.StoreUint32(&q.runningState, stateDumping)
	q.Dump()
}

func TestPopAction1(t *testing.T) {
	q := New()
	q.Run(context.Background())
	q.AddAction(42, time.Now().Add(100*time.Millisecond))
	action := <-q.NextAction()
	if action.Value != 42 {
		t.Fail()
	}
}

func TestPopAction2(t *testing.T) {
	q := New()
	now := time.Now()
	q.actions = []*Action{
		&Action{
			Value:      42,
			ActionTime: now.Add(time.Hour),
			ExpireTime: now,
		},
	}
	q.popAction(context.Background(), now)
	if len(q.actions) != 0 {
		t.Fail()
	}
}

func TestCleanup(t *testing.T) {
	q := New()
	q.Run(context.Background())
	now := time.Now()
	q.AddActionWithExpiry(42, now.Add(100*time.Millisecond), now.Add(200*time.Millisecond))
	q.AddActionWithExpiry(43, now.Add(200*time.Millisecond), now.Add(300*time.Millisecond))
	q.AddAction(44, now.Add(300*time.Millisecond))
	time.Sleep(400 * time.Millisecond)
	action := <-q.NextAction()
	if action.Value != 44 {
		t.Fail()
	}
}
