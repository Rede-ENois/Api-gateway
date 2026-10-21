package gateway

import (
	"context"
	"errors"
)

var ErrQueueFull = errors.New("worker queue is full")

type job struct {
	ctx context.Context
	run func()
}

// Pool keeps request work bounded: full queues are rejected instead of creating
// unbounded goroutines under bursts or a slow ML upstream.
type Pool struct {
	jobs chan job
	done chan struct{}
}

func NewPool(workers, queue int) *Pool {
	if workers < 1 {
		workers = 1
	}
	if queue < 0 {
		queue = 0
	}
	p := &Pool{jobs: make(chan job, queue), done: make(chan struct{})}
	for range workers {
		go func() {
			for {
				select {
				case j := <-p.jobs:
					if j.ctx.Err() == nil {
						j.run()
					}
				case <-p.done:
					return
				}
			}
		}()
	}
	return p
}
func (p *Pool) Submit(ctx context.Context, fn func()) error {
	select {
	case <-p.done:
		return errors.New("worker pool is closed")
	default:
	}
	select {
	case p.jobs <- job{ctx, fn}:
		return nil
	default:
		return ErrQueueFull
	}
}
func (p *Pool) Close() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}
