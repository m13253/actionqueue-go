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
	"container/heap"
	"context"
	"sync/atomic"
	"time"
)

// An Action is an event that fires after `ActionTime`, and expires if not
// handled before `ExpireTime`.
// If `ExpireTime` is zero-value, the action never expires.
type Action struct {
	id         uint64
	Value      interface{}
	ActionTime time.Time
	ExpireTime time.Time
}

type actions []*Action

// A Queue stores actions and triggers actions on time.
type Queue struct {
	actions        actions
	pushActionChan chan *Action
	popActionChan  chan *Action
	isRunning      uint32
	nextID         uint64
	actionTimer    *time.Timer
	expireTimer    *time.Timer
	cleanupTicker  *time.Ticker
}

// New creates a new action queue.
func New() *Queue {
	return &Queue{
		actions:        []*Action{},
		pushActionChan: make(chan *Action, 1),
		popActionChan:  make(chan *Action),
	}
}

// Run runs the action queue in the background.
// To receive next action, use `<-q.NextAction()`.
// To stop the queue, call the cancel function of `ctx`. If you do not need to
// stop the queue, pass `context.Background()` as `ctx`.
func (q *Queue) Run(ctx context.Context) {
	if atomic.CompareAndSwapUint32(&q.isRunning, 0, 1) {
		go q.dispatch(ctx)
	}
}

// AddAction adds a new action to the queue.
func (q *Queue) AddAction(value interface{}, actionTime time.Time) {
	q.pushActionChan <- &Action{
		Value:      value,
		ActionTime: actionTime,
	}
}

// AddActionWithExpiry adds a new action with an `expireTime`.
// An Action is an event that triggers after `ActionTime`, and expires if not
// handled before `ExpireTime`.
func (q *Queue) AddActionWithExpiry(value interface{}, actionTime, expireTime time.Time) {
	now := time.Now()
	if expireTime.IsZero() || now.Before(expireTime) {
		q.pushActionChan <- &Action{
			Value:      value,
			ActionTime: actionTime,
			ExpireTime: expireTime,
		}
	}
}

// NextAction returns a channel for upcoming actions.
// To receive next action, use `<-q.NextAction()`.
// Subsquential calls to `NextAction` returns the same channel.
func (q *Queue) NextAction() chan *Action {
	return q.popActionChan
}

func (q *Queue) dispatch(ctx context.Context) {
	q.actionTimer = time.NewTimer(0)
	q.expireTimer = time.NewTimer(0)
	q.cleanupTicker = time.NewTicker(1 * time.Minute)
	if !q.expireTimer.Stop() {
		<-q.expireTimer.C
	}
	for {
		select {
		case a := <-q.pushActionChan:
			q.pushAction(a)
		case <-q.actionTimer.C:
			q.popAction(ctx)
		case <-q.cleanupTicker.C:
			q.cleanup()
		case <-ctx.Done():
			atomic.StoreUint32(&q.isRunning, 0)
			return
		}
	}
}

func (q *Queue) pushAction(a *Action) {
	now := time.Now()
	if a.ExpireTime.IsZero() || now.Before(a.ExpireTime) {
		a.id = q.nextID
		q.nextID++
		heap.Push(&q.actions, a)
	}
	q.actionTimer.Reset(0)
}

func (q *Queue) popAction(ctx context.Context) {
	if len(q.actions) == 0 {
		return
	}
	nextAction := q.actions[0]
	now := time.Now()
	if !nextAction.ExpireTime.IsZero() && !now.Before(nextAction.ExpireTime) {
		heap.Pop(&q.actions)
		return
	}
	if !nextAction.ActionTime.IsZero() && now.Before(nextAction.ActionTime) {
		waitTime := nextAction.ActionTime.Sub(now)
		q.actionTimer.Reset(waitTime)
		return
	}
	heap.Pop(&q.actions)
	if !nextAction.ExpireTime.IsZero() {
		waitTime := nextAction.ExpireTime.Sub(now)
		q.expireTimer.Reset(waitTime)
	}
	for {
		select {
		case q.popActionChan <- nextAction:
			if !q.expireTimer.Stop() {
				<-q.expireTimer.C
			}
			return
		case a := <-q.pushActionChan:
			q.pushAction(a)
		case <-q.expireTimer.C:
			if !nextAction.ExpireTime.IsZero() && !time.Now().Before(nextAction.ExpireTime) {
				q.actionTimer.Reset(0)
				return
			}
		case <-q.cleanupTicker.C:
			q.cleanup()
		case <-ctx.Done():
			return
		}
	}
}

func (q *Queue) cleanup() {
	now := time.Now()
	finished := true
	for !finished {
		finished = true
		for i := 0; i < len(q.actions); i++ {
			if !q.actions[i].ExpireTime.IsZero() && !now.Before(q.actions[i].ExpireTime) {
				heap.Remove(&q.actions, i)
				finished = false
			}
		}
	}
}

func (a actions) Len() int { return len(a) }

func (a actions) Less(i, j int) bool {
	return a[i].ActionTime.Before(a[j].ActionTime) || (a[i].ActionTime.Equal(a[j].ActionTime) && a[i].id < a[j].id)
}

func (a actions) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

func (a *actions) Push(x interface{}) { *a = append(*a, x.(*Action)) }

func (a *actions) Pop() interface{} {
	result := (*a)[len(*a)-1]
	*a = (*a)[:len(*a)-1]
	return result
}
