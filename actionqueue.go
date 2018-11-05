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

// Package actionqueue provides a timed event queue with expiry support.
//
// Any event is added to the queue with two parameters, ActionTime and
// ExpireTime. The event will be fired upon ActionTime through the the
// NextEvent channel.
//
// The caller may choose either to receive an event immediately, or at any time
// before it is expired. The queue takes care of the ExpireTime, and cancel
// expired events upon expiry.
package actionqueue

import (
	"container/heap"
	"context"
	"sync/atomic"
	"time"
)

const (
	stateStopped  = 0
	stateRunning  = 1
	stateDumping  = 2
	stateStopping = 3
)

// An Action is an event that fires after ActionTime, and expires if not handled
// before ExpireTime.
//
// If ExpireTime is zero-value, the action never expires.
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
	stopRequest    chan struct{}
	stopResponse   chan struct{}
	runningState   uint32
	nextID         uint64
	nextExpiry     time.Time
	actionTimer    *time.Timer
	expireTimer    *time.Timer
}

// New creates a new action queue.
func New() *Queue {
	return &Queue{
		actions:        []*Action{},
		pushActionChan: make(chan *Action, 1),
		popActionChan:  make(chan *Action),
	}
}

// Run runs the action queue in the background. It can be stopped by either
// calling Stop or canceling the context.
//
// To receive next action, use <-q.NextAction().
func (q *Queue) Run(ctx context.Context) {
	if atomic.CompareAndSwapUint32(&q.runningState, stateStopped, stateRunning) {
		q.stopRequest = make(chan struct{})
		q.stopResponse = make(chan struct{})
		go q.run(ctx)
	}
}

// Stop stops a running queue.
func (q *Queue) Stop() {
	if atomic.CompareAndSwapUint32(&q.runningState, stateRunning, stateStopping) {
		close(q.stopRequest)
		<-q.stopResponse
	}
}

// AddAction adds a new action to the queue.
func (q *Queue) AddAction(value interface{}, actionTime time.Time) {
	q.pushActionChan <- &Action{
		Value:      value,
		ActionTime: actionTime,
	}
}

// AddActionWithExpiry adds a new action with an expireTime.
//
// An Action is an event that triggers after ActionTime, and expires if not
// handled before ExpireTime.
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
//
// To receive next action, use <-q.NextAction().
//
// Subsquential calls to NextAction returns the same channel.
func (q *Queue) NextAction() <-chan *Action {
	return q.popActionChan
}

// Dump returns a copy of upcoming actions, useful when saving to disk.
//
// To restore the actions back to a queue, insert them one by one.
//
// Note 1: The Queue MUST be stopped before dumping, or the function will panic.
//
// Note 2: Only one goroutine may call Dump at the same time.
func (q *Queue) Dump() []*Action {
	if !atomic.CompareAndSwapUint32(&q.runningState, stateStopped, stateDumping) {
		panic("actionqueue: the queue must be stopped before dumping")
	}
	result := make([]*Action, len(q.actions))
	copy(result, q.actions)
	atomic.StoreUint32(&q.runningState, stateStopped)
	return result
}

func (q *Queue) run(ctx context.Context) {
	q.actionTimer = time.NewTimer(0)
	q.expireTimer = time.NewTimer(0)
loop:
	for {
		select {
		case a := <-q.pushActionChan:
			q.pushAction(a)
		case now := <-q.actionTimer.C:
			q.popAction(ctx, now)
		case now := <-q.expireTimer.C:
			q.cleanup(now)
		case <-q.stopRequest:
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	q.actionTimer.Stop()
	q.expireTimer.Stop()
	close(q.stopResponse)
	atomic.StoreUint32(&q.runningState, stateStopped)
}

func (q *Queue) pushAction(a *Action) {
	a.id = q.nextID
	q.nextID++
	heap.Push(&q.actions, a)
	q.actionTimer.Reset(0)
	if !a.ExpireTime.IsZero() && (q.nextExpiry.IsZero() || a.ExpireTime.Before(q.nextExpiry)) {
		q.nextExpiry = a.ExpireTime
		waitTime := q.nextExpiry.Sub(time.Now())
		q.expireTimer.Reset(waitTime)
	}
}

func (q *Queue) popAction(ctx context.Context, now time.Time) {
retry:
	if len(q.actions) == 0 {
		return
	}
	nextAction := q.actions[0]
	if !nextAction.ExpireTime.IsZero() && !now.Before(nextAction.ExpireTime) {
		heap.Pop(&q.actions)
		goto retry
	}
	if !nextAction.ActionTime.IsZero() && now.Before(nextAction.ActionTime) {
		waitTime := nextAction.ActionTime.Sub(now)
		q.actionTimer.Reset(waitTime)
		return
	}
	select {
	case q.popActionChan <- nextAction:
		heap.Pop(&q.actions)
		goto retry
	case a := <-q.pushActionChan:
		q.pushAction(a)
		goto retry
	case now = <-q.expireTimer.C:
		q.cleanup(now)
		goto retry
	case <-q.stopRequest:
		return
	case <-ctx.Done():
		return
	}
}

func (q *Queue) cleanup(now time.Time) {
	q.nextExpiry = time.Time{}
	for i := 0; i < len(q.actions); i++ {
		if !q.actions[i].ExpireTime.IsZero() {
			if !now.Before(q.actions[i].ExpireTime) {
				heap.Remove(&q.actions, i)
				i--
			} else if q.nextExpiry.IsZero() || q.actions[i].ExpireTime.Before(q.nextExpiry) {
				q.nextExpiry = q.actions[i].ExpireTime
			}
		}
	}
	if !q.nextExpiry.IsZero() {
		waitTime := q.nextExpiry.Sub(now)
		q.expireTimer.Reset(waitTime)
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
