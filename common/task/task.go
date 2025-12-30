package task

import (
	"context"
	"errors"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type Task struct {
	Name      string
	Interval  time.Duration
	Timeout   time.Duration
	Execute   func(ctx context.Context) error
	OnTimeout func()
	Access    sync.RWMutex
	Running   bool
	Stop      chan struct{}
}

func (t *Task) Start(first bool) error {
	t.Access.Lock()
	if t.Running {
		t.Access.Unlock()
		return nil
	}
	t.Running = true
	t.Stop = make(chan struct{})
	t.Access.Unlock()

	go func() {
		defer t.safeStop()

		execToken := make(chan struct{}, 1)
		execToken <- struct{}{}

		run := func() {
			select {
			case <-t.Stop:
				return
			default:
			}

			select {
			case <-execToken:
				go func() {
					defer func() { execToken <- struct{}{} }()
					if err := t.ExecuteWithTimeout(); err != nil {
						log.Errorf("Task %s execution error: %v", t.Name, err)
						t.safeStop()
					}
				}()
			default:
				log.Warnf("Task %s previous execution still running, skip", t.Name)
			}
		}

		if first {
			run()
		}

		timer := time.NewTimer(t.Interval)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				run()
				timer.Reset(t.Interval)
			case <-t.Stop:
				return
			}
		}
	}()

	return nil
}

func (t *Task) ExecuteWithTimeout() error {
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = min(3*t.Interval, 5*time.Minute)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := t.Execute(ctx)
	if err == nil {
		return nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		log.WithField("timeout", timeout).Errorf("Task %s execution timed out", t.Name)
		if t.OnTimeout != nil {
			t.OnTimeout()
		}
		return nil
	}

	return err
}

func (t *Task) safeStop() {
	t.Access.Lock()
	if t.Running {
		t.Running = false
		close(t.Stop)
	}
	t.Access.Unlock()
}

func (t *Task) Close() {
	t.safeStop()
	log.Infof("Task %s stopped", t.Name)
}
